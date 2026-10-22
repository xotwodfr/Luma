#include <QtTest>

#include "Store.h"

static Message msg(const QString &conv, qint64 seq, const QString &body)
{
    Message m;
    m.id = QUuid::createUuid().toString(QUuid::WithoutBraces);
    m.conversationId = conv;
    m.seq = seq;
    m.body = body;
    m.kind = QStringLiteral("text");
    m.createdAt = QDateTime::currentDateTimeUtc();
    return m;
}

static Conversation conv(const QString &id, const QDateTime &activity)
{
    Conversation c;
    c.id = id;
    c.kind = QStringLiteral("direct");
    c.lastActivityAt = activity;
    return c;
}

class TestStore : public QObject {
    Q_OBJECT
    QTemporaryDir dir;
    QString path() const { return dir.filePath(QStringLiteral("luma.db")); }

private slots:
    void init() { QFile::remove(path()); }

    void upsertIsIdempotent()
    {
        Store s(path());
        s.upsertMessage(msg(QStringLiteral("c1"), 1, QStringLiteral("first")));
        s.upsertMessage(msg(QStringLiteral("c1"), 1, QStringLiteral("first, again")));
        s.upsertMessage(msg(QStringLiteral("c1"), 2, QStringLiteral("second")));
        const auto ms = s.messages(QStringLiteral("c1"), 50);
        QCOMPARE(ms.size(), 2);
        QCOMPARE(ms[0].seq, 1);
        QCOMPARE(ms[0].body, QStringLiteral("first, again"));
        QCOMPARE(ms[1].seq, 2);
    }

    void messagesReturnsLatestPageAscending()
    {
        Store s(path());
        for (int i = 1; i <= 10; ++i)
            s.upsertMessage(msg(QStringLiteral("c1"), i, QString::number(i)));
        const auto ms = s.messages(QStringLiteral("c1"), 3);
        QCOMPARE(ms.size(), 3);
        QCOMPARE(ms[0].seq, 8);
        QCOMPARE(ms[2].seq, 10);
    }

    void conversationsOrderedByActivity()
    {
        Store s(path());
        const auto now = QDateTime::currentDateTimeUtc();
        s.upsertConversation(conv(QStringLiteral("old"), now.addSecs(-60)));
        s.upsertConversation(conv(QStringLiteral("new"), now));
        const auto cs = s.conversations();
        QCOMPARE(cs.size(), 2);
        QCOMPARE(cs[0].id, QStringLiteral("new"));
    }

    void outboxPersistsAcrossReopen()
    {
        const QUuid id = QUuid::createUuid();
        {
            Store s(path());
            s.addOutbox({id, QStringLiteral("c1"), QStringLiteral("pending text"), QDateTime::currentDateTimeUtc(), OutboxItem::Pending});
            s.setOutboxState(id, OutboxItem::Failed);
        }
        Store reopened(path());
        const auto out = reopened.outbox();
        QCOMPARE(out.size(), 1);
        QCOMPARE(out[0].clientId, id);
        QCOMPARE(out[0].body, QStringLiteral("pending text"));
        QCOMPARE(out[0].state, OutboxItem::Failed);
        reopened.removeOutbox(id);
        QVERIFY(reopened.outbox().isEmpty());
    }

    void cursorNullVersusZero()
    {
        Store s(path());
        QVERIFY(!s.cursor().has_value());
        s.setCursor(0);
        QCOMPARE(s.cursor().value(), 0);
        s.setCursor(42);
        Store reopened(path());
        QCOMPARE(reopened.cursor().value(), 42);
    }

    void clearWipesEverything()
    {
        Store s(path());
        s.upsertConversation(conv(QStringLiteral("c1"), QDateTime::currentDateTimeUtc()));
        s.upsertMessage(msg(QStringLiteral("c1"), 1, QStringLiteral("x")));
        s.addOutbox({QUuid::createUuid(), QStringLiteral("c1"), QStringLiteral("y"), QDateTime::currentDateTimeUtc(), OutboxItem::Pending});
        s.setCursor(5);
        s.clear();
        QVERIFY(s.conversations().isEmpty());
        QVERIFY(s.messages(QStringLiteral("c1"), 10).isEmpty());
        QVERIFY(s.outbox().isEmpty());
        QVERIFY(!s.cursor().has_value());
    }

    void conversationRoundTripsJson()
    {
        const QByteArray json = R"({"id":"c1","kind":"direct","title":null,"created_at":"2026-10-22T10:00:00Z",
            "last_seq":3,"last_activity_at":"2026-10-22T10:05:00Z","last_read_seq":2,"unread":1,"muted":false,
            "archived":false,"left":false,
            "last_message":{"id":"m3","conversation_id":"c1","seq":3,"sender_id":"u2","client_id":"k3","kind":"text",
                            "body":"hi","meta":{},"created_at":"2026-10-22T10:05:00Z"},
            "members":[{"user_id":"u1","username":"alice","display_name":"Alice","role":"member","last_read_seq":3,"last_delivered_seq":3}]})";
        const auto c = Conversation::fromJson(QJsonDocument::fromJson(json).object());
        QCOMPARE(c.unread, 1);
        QVERIFY(c.lastMessage.has_value());
        QCOMPARE(c.lastMessage->body, QStringLiteral("hi"));
        QCOMPARE(c.members.size(), 1);
        QCOMPARE(c.members[0].displayName, QStringLiteral("Alice"));
        const auto again = Conversation::fromJson(c.toJson());
        QCOMPARE(again.lastMessage->seq, 3);
        QCOMPARE(again.members[0].lastReadSeq, 3);
        QCOMPARE(again.lastActivityAt, c.lastActivityAt);
    }
};

QTEST_GUILESS_MAIN(TestStore)
#include "tst_store.moc"
