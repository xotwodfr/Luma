#include <QtTest>

#include "Store.h"
#include "Sync.h"

// ── Fakes ────────────────────────────────────────────────────────────────────
class FakeTransport : public Transport {
public:
    QList<QJsonObject> sent;
    int starts = 0;
    std::optional<qint64> startedWith;
    void start(const QString &, std::optional<qint64> cursor) override { ++starts; startedWith = cursor; setState(State::Connecting); }
    void stop() override { setState(State::Idle); }
    void sendFrame(const QJsonObject &f) override { sent.append(f); }
    void deliver(const QJsonObject &f)
    {
        if (f[u"t"].toString() == u"ready")
            setState(State::Online);
        emit frame(f);
    }
    void close(int code) { setState(State::Idle); emit closed(code); }
    void failReconnect() { setState(State::Reconnecting); }
    void setStateForTest(State s) { setState(s); }
    QList<QJsonObject> sentOfType(const QString &t) const
    {
        QList<QJsonObject> out;
        for (const auto &f : sent)
            if (f[u"t"].toString() == t)
                out.append(f);
        return out;
    }
};

class FakeApi : public ApiClient {
public:
    FakeApi() : ApiClient(QUrl(QStringLiteral("http://fake"))) {}
    QJsonArray listResult;
    QHash<QString, QJsonObject> byId;
    QStringList fetched;
    bool defer = false;                   // hold callbacks to simulate replies arriving late
    QList<std::function<void()>> held;
    int meCalls = 0;
    int meStatus = 200;
    void me(Callback cb) override
    {
        ++meCalls;
        ApiResult r;
        r.status = meStatus;
        if (meStatus == 401)
            emit unauthorized();
        cb(r);
    }
    void conversations(Callback cb) override
    {
        if (defer) {
            held.append([this, cb] { defer = false; conversations(cb); });
            return;
        }
        ApiResult r;
        r.status = 200;
        r.body = QJsonDocument(QJsonObject{{QStringLiteral("conversations"), listResult}});
        cb(r);
    }
    void conversation(const QString &id, Callback cb) override
    {
        fetched.append(id);
        if (defer) {
            held.append([this, id, cb] { defer = false; fetched.removeLast(); conversation(id, cb); });
            return;
        }
        ApiResult r;
        r.status = byId.contains(id) ? 200 : 404;
        r.body = QJsonDocument(byId.value(id));
        cb(r);
    }
};

static QJsonObject parse(const char *json) { return QJsonDocument::fromJson(QByteArray(json)).object(); }

static QJsonObject convJson(const QString &id, qint64 lastSeq, qint64 lastRead, qint64 unread)
{
    Conversation c;
    c.id = id;
    c.kind = QStringLiteral("direct");
    c.lastSeq = lastSeq;
    c.lastReadSeq = lastRead;
    c.unread = unread;
    c.lastActivityAt = QDateTime::currentDateTimeUtc();
    c.members = {{QStringLiteral("me"), QStringLiteral("me"), QStringLiteral("Me"), QStringLiteral("member"), 0, 0},
                 {QStringLiteral("bob"), QStringLiteral("bob"), QStringLiteral("Bob"), QStringLiteral("member"), 0, 0}};
    return c.toJson();
}

static QJsonObject messageJson(const QString &conv, qint64 seq, const QString &sender, const QString &clientId, const QString &body)
{
    Message m;
    m.id = QStringLiteral("m-%1-%2").arg(conv).arg(seq);
    m.conversationId = conv;
    m.seq = seq;
    m.senderId = sender;
    m.clientId = clientId;
    m.kind = QStringLiteral("text");
    m.body = body;
    m.createdAt = QDateTime::currentDateTimeUtc();
    return m.toJson();
}

static QJsonObject evt(qint64 seq, const QString &type, const QJsonObject &data)
{
    return {{QStringLiteral("t"), QStringLiteral("event")},
            {QStringLiteral("d"), QJsonObject{{QStringLiteral("seq"), seq}, {QStringLiteral("type"), type}, {QStringLiteral("data"), data}}}};
}

static QJsonObject ready(qint64 cursor)
{
    return {{QStringLiteral("t"), QStringLiteral("ready")}, {QStringLiteral("d"), QJsonObject{{QStringLiteral("cursor"), cursor}}}};
}

// Turns {"t":"event","d.seq":2,"d.data.message.body":"x"} into nested JSON.
static QJsonObject unflatten(const QJsonObject &flat, const QHash<QString, QString> &vars)
{
    QVariantMap root;
    for (auto it = flat.begin(); it != flat.end(); ++it) {
        QJsonValue v = it.value();
        if (v.isString() && v.toString().startsWith(u'$'))
            v = vars.value(v.toString().mid(1));
        const QStringList path = it.key().split(u'.');
        std::function<void(QVariantMap &, int)> put = [&](QVariantMap &m, int i) {
            if (i == path.size() - 1) {
                m[path[i]] = v.toVariant();
                return;
            }
            QVariantMap child = m.value(path[i]).toMap();
            put(child, i + 1);
            m[path[i]] = child;
        };
        put(root, 0);
    }
    return QJsonObject::fromVariantMap(root);
}

// ── Tests ────────────────────────────────────────────────────────────────────
class TestSync : public QObject {
    Q_OBJECT
    QTemporaryDir dir;
    QString path() const { return dir.filePath(QStringLiteral("sync.db")); }

private slots:
    void init()
    {
        QFile::remove(path());
        Sync::typingTtlMs = 150;
    }

    void freshInstallSendsNullCursor()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        QCOMPARE(t.starts, 1);
        QVERIFY(!t.startedWith.has_value());
    }

    void ackAndEventDedupe()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(1));
        const QUuid id = sync.send(QStringLiteral("c1"), QStringLiteral("hello"));
        QCOMPARE(store.outbox().size(), 1);
        const auto sends = t.sentOfType(QStringLiteral("send"));
        QCOMPARE(sends.size(), 1);
        const QString cid = id.toString(QUuid::WithoutBraces);
        QCOMPARE(sends[0][u"d"][u"client_id"].toString(), cid);
        QCOMPARE(sends[0][u"id"].toString(), cid);

        const QJsonObject m = messageJson(QStringLiteral("c1"), 1, QStringLiteral("me"), cid, QStringLiteral("hello"));
        t.deliver(evt(2, QStringLiteral("message.created"), {{QStringLiteral("conversation_id"), QStringLiteral("c1")}, {QStringLiteral("message"), m}}));
        t.deliver({{QStringLiteral("t"), QStringLiteral("ack")}, {QStringLiteral("id"), cid}, {QStringLiteral("d"), QJsonObject{{QStringLiteral("message"), m}}}});
        QCOMPARE(store.messages(QStringLiteral("c1"), 10).size(), 1);
        QVERIFY(store.outbox().isEmpty());
        QCOMPARE(sync.pending(QStringLiteral("c1")).size(), 0);
        QCOMPARE(store.cursor().value(), 2);
    }

    void outboxSurvivesRestart()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        FakeApi api;
        QUuid id;
        {
            FakeTransport t;
            Sync sync(store, api, t);
            sync.start(QStringLiteral("tok"), QStringLiteral("me"));
            id = sync.send(QStringLiteral("c1"), QStringLiteral("written offline")); // never ready → not sent
            QVERIFY(t.sentOfType(QStringLiteral("send")).isEmpty());
        }
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        QCOMPARE(sync.pending(QStringLiteral("c1")).size(), 1);
        t.deliver(ready(0));
        const auto sends = t.sentOfType(QStringLiteral("send"));
        QCOMPARE(sends.size(), 1);
        QCOMPARE(sends[0][u"d"][u"client_id"].toString(), id.toString(QUuid::WithoutBraces));
        t.deliver(ready(0)); // a second ready (reconnect) resends the still-pending item, same client_id
        QCOMPARE(t.sentOfType(QStringLiteral("send")).size(), 2);
        QCOMPARE(t.sentOfType(QStringLiteral("send"))[1][u"d"][u"client_id"], sends[0][u"d"][u"client_id"]);
    }

    void resyncReloads()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("stale"), 9, 4, 5)));
        store.setCursor(3);
        FakeApi api;
        api.listResult = {convJson(QStringLiteral("c1"), 9, 9, 0)};
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        QCOMPARE(t.startedWith.value(), 3);
        t.deliver({{QStringLiteral("t"), QStringLiteral("resync_required")}, {QStringLiteral("d"), QJsonObject{{QStringLiteral("cursor"), 10}}}});
        const auto cs = store.conversations();
        QCOMPARE(cs.size(), 1);
        QCOMPARE(cs[0].id, QStringLiteral("c1"));
        QCOMPARE(cs[0].unread, 0);
        QCOMPARE(store.cursor().value(), 10);
    }

    void unknownConversationFetched()
    {
        Store store(path());
        FakeApi api;
        api.byId[QStringLiteral("cx")] = convJson(QStringLiteral("cx"), 1, 0, 1);
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(0));
        const QJsonObject m = messageJson(QStringLiteral("cx"), 1, QStringLiteral("bob"), QStringLiteral("k1"), QStringLiteral("hi"));
        t.deliver(evt(1, QStringLiteral("message.created"), {{QStringLiteral("conversation_id"), QStringLiteral("cx")}, {QStringLiteral("message"), m}}));
        QCOMPARE(api.fetched, QStringList{QStringLiteral("cx")});
        QVERIFY(store.conversation(QStringLiteral("cx")).has_value());
        QCOMPARE(store.messages(QStringLiteral("cx"), 10).size(), 1);
    }

    void unreadAndMarkRead()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(0));
        for (int i = 1; i <= 3; ++i)
            t.deliver(evt(i, QStringLiteral("message.created"),
                            {{QStringLiteral("conversation_id"), QStringLiteral("c1")},
                             {QStringLiteral("message"), messageJson(QStringLiteral("c1"), i, QStringLiteral("bob"), QUuid::createUuid().toString(), QStringLiteral("m"))}}));
        QCOMPARE(store.conversation(QStringLiteral("c1"))->unread, 3);
        QTRY_COMPARE(t.sentOfType(QStringLiteral("delivered")).size(), 1); // debounced
        QCOMPARE(t.sentOfType(QStringLiteral("delivered"))[0][u"d"][u"seq"].toInteger(), 3);
        sync.markRead(QStringLiteral("c1"));
        QCOMPARE(store.conversation(QStringLiteral("c1"))->unread, 0);
        QCOMPARE(t.sentOfType(QStringLiteral("read"))[0][u"d"][u"seq"].toInteger(), 3);
        sync.markRead(QStringLiteral("c1")); // nothing new → no extra frame
        QCOMPARE(t.sentOfType(QStringLiteral("read")).size(), 1);
    }

    void staleEventsIgnored()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        store.setCursor(5);
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(evt(5, QStringLiteral("message.created"),
                        {{QStringLiteral("conversation_id"), QStringLiteral("c1")},
                         {QStringLiteral("message"), messageJson(QStringLiteral("c1"), 1, QStringLiteral("bob"), QStringLiteral("k"), QStringLiteral("old"))}}));
        QVERIFY(store.messages(QStringLiteral("c1"), 10).isEmpty());
        QCOMPARE(store.cursor().value(), 5);
    }

    void permanentErrorMarksFailedAndRetryResends()
    {
        Store store(path());
        store.upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(0));
        const QUuid id = sync.send(QStringLiteral("c1"), QStringLiteral("blocked?"));
        t.deliver({{QStringLiteral("t"), QStringLiteral("error")}, {QStringLiteral("id"), id.toString(QUuid::WithoutBraces)},
                   {QStringLiteral("d"), QJsonObject{{QStringLiteral("code"), QStringLiteral("blocked")}, {QStringLiteral("message"), QStringLiteral("x")}}}});
        QCOMPARE(store.outbox()[0].state, OutboxItem::Failed);
        sync.retry(id);
        QCOMPARE(store.outbox()[0].state, OutboxItem::Pending);
        QCOMPARE(t.sentOfType(QStringLiteral("send")).size(), 2);
    }

    void rejectsInvalidText()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        QVERIFY(sync.send(QStringLiteral("c1"), QStringLiteral("   \n")).isNull());
        QVERIFY(sync.send(QStringLiteral("c1"), QString(8001, u'x')).isNull());
        QVERIFY(store.outbox().isEmpty());
    }

    void revokedSignsOut()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        QSignalSpy spy(&sync, &Sync::signedOut);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.close(4003);
        QCOMPARE(spy.size(), 1);
        emit api.unauthorized();
        QCOMPARE(spy.size(), 2);
    }

    // A revoked token makes every handshake fail with HTTP 401, which a WebSocket only reports as a
    // generic failure; a throttled REST check turns that into a clean sign-out.
    void reconnectFailureChecksSession()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        QSignalSpy spy(&sync, &Sync::signedOut);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.failReconnect();
        QCOMPARE(api.meCalls, 1);
        QCOMPARE(spy.size(), 0); // 200: just a network problem, keep retrying
        t.setStateForTest(Transport::State::Connecting);
        t.failReconnect();
        QCOMPARE(api.meCalls, 1); // throttled
        Sync::sessionCheckEveryMs = 0;
        api.meStatus = 401;
        t.setStateForTest(Transport::State::Connecting);
        t.failReconnect();
        QCOMPARE(spy.size(), 1);
        QCOMPARE(t.state(), Transport::State::Idle); // stopped, no reconnect loop
        Sync::sessionCheckEveryMs = 30000;
    }

    // A reply that started under one session must not write into the store after sign-out
    // (the next account would see the previous one's data).
    void staleRepliesAfterStopAreDropped()
    {
        Store store(path());
        FakeApi api;
        api.listResult = {convJson(QStringLiteral("old-account"), 1, 0, 1)};
        api.defer = true;
        FakeTransport t;
        Sync sync(store, api, t);
        store.setCursor(1);
        sync.start(QStringLiteral("tok"), QStringLiteral("me")); // triggers reloadConversations
        QCOMPARE(api.held.size(), 1);
        sync.stop();
        store.clear();
        for (const auto &f : api.held)
            f();
        QVERIFY(store.conversations().isEmpty());
    }

    void failedConversationFetchIsRetried()
    {
        Store store(path());
        FakeApi api; // "cx" unknown at first → 404
        FakeTransport t;
        Sync::retryFetchMs = 50;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(0));
        t.deliver(evt(1, QStringLiteral("message.created"),
                      {{QStringLiteral("conversation_id"), QStringLiteral("cx")},
                       {QStringLiteral("message"), messageJson(QStringLiteral("cx"), 1, QStringLiteral("bob"), QStringLiteral("k"), QStringLiteral("hi"))}}));
        QVERIFY(!store.conversation(QStringLiteral("cx")).has_value());
        api.listResult = {convJson(QStringLiteral("cx"), 1, 0, 1)};
        QTRY_VERIFY(store.conversation(QStringLiteral("cx")).has_value());
    }

    void typingExpires()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver({{QStringLiteral("t"), QStringLiteral("typing")},
                   {QStringLiteral("d"), QJsonObject{{QStringLiteral("conversation_id"), QStringLiteral("c1")}, {QStringLiteral("user_id"), QStringLiteral("bob")}}}});
        QCOMPARE(sync.typingUsers(QStringLiteral("c1")), QStringList{QStringLiteral("bob")});
        QTRY_VERIFY(sync.typingUsers(QStringLiteral("c1")).isEmpty());
    }

    void typingThrottled()
    {
        Store store(path());
        FakeApi api;
        FakeTransport t;
        Sync sync(store, api, t);
        sync.start(QStringLiteral("tok"), QStringLiteral("me"));
        t.deliver(ready(0));
        for (int i = 0; i < 5; ++i)
            sync.typing(QStringLiteral("c1"));
        QCOMPARE(t.sentOfType(QStringLiteral("typing")).size(), 1);
    }

    // The server-side golden vector, applied to the client: what bob and alice receive must parse and apply.
    void protocolVectorApplies()
    {
        QFile f(QStringLiteral(LUMA_VECTORS_DIR "/realtime-basic.json"));
        QVERIFY(f.open(QIODevice::ReadOnly));
        const QJsonArray steps = QJsonDocument::fromJson(f.readAll()).object()[u"steps"].toArray();
        const QHash<QString, QString> vars{{QStringLiteral("dm"), QStringLiteral("c1")}, {QStringLiteral("uuid1"), QStringLiteral("k1")}};

        Store bobStore(dir.filePath(QStringLiteral("bob.db")));
        Store aliceStore(dir.filePath(QStringLiteral("alice.db")));
        for (Store *s : {&bobStore, &aliceStore})
            s->upsertConversation(Conversation::fromJson(convJson(QStringLiteral("c1"), 0, 0, 0)));
        FakeApi api;
        api.byId[QStringLiteral("c1")] = convJson(QStringLiteral("c1"), 0, 0, 0);
        FakeTransport bobT, aliceT;
        Sync bob(bobStore, api, bobT), alice(aliceStore, api, aliceT);
        bob.start(QStringLiteral("t"), QStringLiteral("bob"));
        alice.start(QStringLiteral("t"), QStringLiteral("me"));
        bobT.deliver(ready(0));
        aliceT.deliver(ready(0));

        int applied = 0;
        for (const auto &s : steps) {
            const QJsonObject step = s.toObject();
            const QJsonObject expect = step[u"expect"].toObject();
            if (expect[u"t"].toString() != u"event")
                continue;
            QJsonObject frame = unflatten(expect, vars);
            QJsonObject d = frame[u"d"].toObject();
            QJsonObject data = d[u"data"].toObject();
            if (d[u"type"].toString() == u"message.created") { // vectors list only asserted fields; complete the message
                QJsonObject m = data[u"message"].toObject();
                QJsonObject full = messageJson(QStringLiteral("c1"), m[u"seq"].toInteger(), QStringLiteral("me"), QStringLiteral("k1"), m[u"body"].toString());
                data[u"message"] = full;
                data[u"conversation_id"] = QStringLiteral("c1");
            }
            if (d[u"type"].toString() == u"receipt.updated") {
                data[u"conversation_id"] = QStringLiteral("c1");
                data[u"user_id"] = QStringLiteral("bob");
            }
            if (!d.contains(u"seq")) // vectors assert only some fields; give unasserted events the next seq
                d[u"seq"] = (step[u"actor"].toString() == u"bob" ? bobStore : aliceStore).cursor().value_or(0) + 1;
            d[u"data"] = data;
            frame[u"d"] = d;
            (step[u"actor"].toString() == u"bob" ? bobT : aliceT).deliver(frame);
            ++applied;
        }
        QCOMPARE(applied, 2);
        const auto msgs = bobStore.messages(QStringLiteral("c1"), 10);
        QCOMPARE(msgs.size(), 1);
        QCOMPARE(msgs[0].body, QStringLiteral("Are we still on for the lake?"));
        const auto conv = aliceStore.conversation(QStringLiteral("c1"));
        const auto bobMember = std::find_if(conv->members.begin(), conv->members.end(), [](const Member &m) { return m.userId == u"bob"; });
        QCOMPARE(bobMember->lastReadSeq, 1);
    }
};

QTEST_GUILESS_MAIN(TestSync)
#include "tst_sync.moc"
