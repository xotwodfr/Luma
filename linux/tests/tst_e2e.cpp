// End-to-end against a real luma-server. Run via linux/scripts/e2e.sh (skipped otherwise).
#include <QtTest>

#include "ApiClient.h"
#include "RealtimeClient.h"
#include "Store.h"
#include "Sync.h"

struct Client {
    QTemporaryDir dir;
    std::unique_ptr<Store> store;
    std::unique_ptr<ApiClient> api;
    std::unique_ptr<RealtimeClient> rt;
    std::unique_ptr<Sync> sync;
    QString userId, token;

    void boot(const QUrl &base)
    {
        store = std::make_unique<Store>(dir.filePath(QStringLiteral("luma.db")));
        api = std::make_unique<ApiClient>(base);
        api->setToken(token);
        rt = std::make_unique<RealtimeClient>(api->realtimeUrl());
        sync = std::make_unique<Sync>(*store, *api, *rt);
        sync->start(token, userId);
    }
};

class TestE2E : public QObject {
    Q_OBJECT
    QUrl base;
    QStringList invites;

    std::unique_ptr<Client> registerUser(const QString &invite, const QString &username)
    {
        auto c = std::make_unique<Client>();
        ApiClient api(base);
        bool done = false;
        api.registerAccount(invite, username, username, QStringLiteral("e2e-password-1"), [&](const ApiResult &r) {
            QVERIFY2(r.ok(), qPrintable(r.errorCode + u' ' + r.errorMessage));
            c->token = r.body[u"token"].toString();
            c->userId = r.body[u"user"][u"id"].toString();
            done = true;
        });
        [&] { QTRY_VERIFY_WITH_TIMEOUT(done, 10000); }();
        c->boot(base);
        return c;
    }

private slots:
    void initTestCase()
    {
        base = QUrl(qEnvironmentVariable("LUMA_E2E_URL"));
        invites = qEnvironmentVariable("LUMA_E2E_INVITES").split(u',', Qt::SkipEmptyParts);
        if (base.isEmpty() || invites.size() < 2)
            QSKIP("set LUMA_E2E_URL and LUMA_E2E_INVITES (use linux/scripts/e2e.sh)");
    }

    void fullFlow()
    {
        auto alice = registerUser(invites[0], QStringLiteral("alice_e2e"));
        auto bob = registerUser(invites[1], QStringLiteral("bob_e2e"));
        QTRY_VERIFY_WITH_TIMEOUT(alice->sync->isReady() && bob->sync->isReady(), 10000);

        // Open a DM over REST and send over the WebSocket.
        QString conv;
        alice->api->openDirect(QStringLiteral("bob_e2e"), [&](const ApiResult &r) { conv = r.body[u"id"].toString(); });
        QTRY_VERIFY(!conv.isEmpty());
        QVERIFY(!alice->sync->send(conv, QStringLiteral("hello bob")).isNull());
        QTRY_COMPARE(bob->store->messages(conv, 10).size(), 1);
        QCOMPARE(bob->store->messages(conv, 10)[0].body, QStringLiteral("hello bob"));
        QTRY_VERIFY(alice->store->outbox().isEmpty());
        QTRY_COMPARE(bob->store->conversation(conv)->unread, 1);

        // Read receipt reaches the sender.
        bob->sync->markRead(conv);
        auto bobReadSeqAtAlice = [&] {
            const auto c = alice->store->conversation(conv);
            for (const auto &m : c->members)
                if (m.userId == bob->userId)
                    return m.lastReadSeq;
            return qint64(-1);
        };
        QTRY_COMPARE(bobReadSeqAtAlice(), 1);

        // Offline sends wait in the outbox, then arrive exactly once, in order, after reconnect.
        alice->rt->stop();
        alice->sync->send(conv, QStringLiteral("offline one"));
        alice->sync->send(conv, QStringLiteral("offline two"));
        QCOMPARE(alice->store->outbox().size(), 2);
        QTest::qWait(300);
        QCOMPARE(bob->store->messages(conv, 10).size(), 1);
        alice->sync->start(alice->token, alice->userId);
        QTRY_COMPARE_WITH_TIMEOUT(bob->store->messages(conv, 10).size(), 3, 10000);
        const auto msgs = bob->store->messages(conv, 10);
        QCOMPARE(msgs[1].body, QStringLiteral("offline one"));
        QCOMPARE(msgs[2].body, QStringLiteral("offline two"));
        QCOMPARE(msgs[1].seq, 2);
        QCOMPARE(msgs[2].seq, 3);
        QTRY_VERIFY(alice->store->outbox().isEmpty());
        QTest::qWait(300);
        QCOMPARE(bob->store->messages(conv, 10).size(), 3); // no duplicates after the ack settles

        // A fresh install (cursor null) resyncs over REST instead of replaying.
        Client fresh;
        fresh.token = bob->token;
        fresh.userId = bob->userId;
        fresh.boot(base);
        QTRY_VERIFY_WITH_TIMEOUT(fresh.store->conversation(conv).has_value(), 10000);
        QCOMPARE(fresh.store->conversation(conv)->lastMessage->seq, 3);
        QTRY_VERIFY(fresh.store->cursor().has_value());

        // Revoking a session signs that client out.
        QSignalSpy signedOut(alice->sync.get(), &Sync::signedOut);
        bool loggedOut = false;
        alice->api->logout([&](const ApiResult &r) { loggedOut = r.status == 204; });
        QTRY_VERIFY(loggedOut);
        QTRY_COMPARE_WITH_TIMEOUT(signedOut.size(), 1, 5000);
        QCOMPARE(signedOut[0][0].toString(), QStringLiteral("revoked"));
    }
};

QTEST_GUILESS_MAIN(TestE2E)
#include "tst_e2e.moc"
