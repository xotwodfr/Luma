#include <QtTest>

#include "Format.h"
#include "MessageListModel.h"

static Message m(qint64 seq, const QString &sender, const QDateTime &at, const QString &body = QStringLiteral("x"))
{
    Message msg;
    msg.id = QStringLiteral("m%1").arg(seq);
    msg.clientId = QStringLiteral("c%1").arg(seq);
    msg.conversationId = QStringLiteral("conv");
    msg.seq = seq;
    msg.senderId = sender;
    msg.kind = QStringLiteral("text");
    msg.body = body;
    msg.createdAt = at;
    return msg;
}

static Conversation dm(qint64 bobRead, qint64 bobDelivered)
{
    Conversation c;
    c.id = QStringLiteral("conv");
    c.kind = QStringLiteral("direct");
    c.members = {{QStringLiteral("me"), QStringLiteral("me"), QStringLiteral("Me"), QStringLiteral("member"), 0, 0},
                 {QStringLiteral("bob"), QStringLiteral("bob"), QStringLiteral("Bob Lake"), QStringLiteral("member"), bobRead, bobDelivered}};
    return c;
}

class TestModels : public QObject {
    Q_OBJECT
private slots:
    void initTestCase() { QLocale::setDefault(QLocale(QLocale::English, QLocale::UnitedKingdom)); }

    void groupingPositions()
    {
        const QDateTime t0(QDate(2026, 10, 22), QTime(14, 0));
        const QList<Message> msgs = {
            m(1, QStringLiteral("bob"), t0),
            m(2, QStringLiteral("bob"), t0.addSecs(60)),
            m(3, QStringLiteral("bob"), t0.addSecs(120)),
            m(4, QStringLiteral("bob"), t0.addSecs(120 + 240)), // 4-minute gap → new group
            m(5, QStringLiteral("me"), t0.addSecs(400)),
        };
        const auto rows = MessageListModel::buildRows(msgs, {}, dm(0, 0), QStringLiteral("me"), t0.addSecs(500));
        QStringList pos;
        for (const auto &r : rows)
            pos << r.groupPos;
        QCOMPARE(pos, (QStringList{u"first"_s, u"middle"_s, u"last"_s, u"single"_s, u"single"_s}));
        QVERIFY(rows[0].showDate);
        QCOMPARE(rows[0].dateLabel, u"Today"_s);
        QVERIFY(!rows[1].showDate);
        QCOMPARE(rows[0].time, u"14:00"_s);
    }

    void receiptsFromWatermarks()
    {
        const QDateTime t0 = QDateTime::currentDateTime();
        const QList<Message> msgs = {m(1, u"me"_s, t0), m(2, u"me"_s, t0), m(3, u"me"_s, t0)};
        OutboxItem pending{QUuid::createUuid(), u"conv"_s, u"later"_s, t0, OutboxItem::Pending};
        OutboxItem failed{QUuid::createUuid(), u"conv"_s, u"nope"_s, t0, OutboxItem::Failed};
        const auto rows = MessageListModel::buildRows(msgs, {pending, failed}, dm(1, 2), u"me"_s, t0);
        QStringList r;
        for (const auto &row : rows)
            r << row.receipt;
        QCOMPARE(r, (QStringList{u"read"_s, u"delivered"_s, u"sent"_s, u"pending"_s, u"failed"_s}));
        QVERIFY(rows[3].mine && rows[4].mine);
    }

    void pendingKeyMatchesConfirmedKey()
    {
        // The pending bubble and the confirmed message share a key, so the model updates the row in place.
        const QUuid id = QUuid::createUuid();
        OutboxItem pending{id, u"conv"_s, u"hi"_s, QDateTime::currentDateTime(), OutboxItem::Pending};
        Message confirmed = m(1, u"me"_s, QDateTime::currentDateTime(), u"hi"_s);
        confirmed.clientId = id.toString(QUuid::WithoutBraces);
        const auto before = MessageListModel::buildRows({}, {pending}, dm(0, 0), u"me"_s, QDateTime::currentDateTime());
        const auto after = MessageListModel::buildRows({confirmed}, {}, dm(0, 0), u"me"_s, QDateTime::currentDateTime());
        QCOMPARE(before[0].key, after[0].key);
    }

    void listTimeLabels()
    {
        const QDateTime now(QDate(2026, 10, 22), QTime(15, 0)); // a Thursday
        QCOMPARE(fmt::listTime(QDateTime(QDate(2026, 10, 22), QTime(9, 5)), now), u"09:05"_s);
        QCOMPARE(fmt::listTime(QDateTime(QDate(2026, 10, 21), QTime(23, 0)), now), u"Yesterday"_s);
        QCOMPARE(fmt::listTime(QDateTime(QDate(2026, 10, 19), QTime(8, 0)), now), u"Mon"_s);
        QCOMPARE(fmt::listTime(QDateTime(QDate(2026, 10, 2), QTime(8, 0)), now), u"2 Oct"_s);
        QCOMPARE(fmt::listTime(QDateTime(QDate(2025, 12, 30), QTime(8, 0)), now), u"30 Dec 2025"_s);
    }

    void initials()
    {
        QCOMPARE(fmt::initials(u"Mira Okafor"_s), u"MO"_s);
        QCOMPARE(fmt::initials(u"dad"_s), u"D"_s);
        QCOMPARE(fmt::initials(u"  Ana  María Ruiz "_s), u"AR"_s);
        QCOMPARE(fmt::initials(u""_s), u"?"_s);
    }
};

QTEST_GUILESS_MAIN(TestModels)
#include "tst_models.moc"
