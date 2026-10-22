#include "Types.h"

static QDateTime ts(const QJsonValue &v) { return QDateTime::fromString(v.toString(), Qt::ISODateWithMs); }
static QString iso(const QDateTime &d) { return d.toUTC().toString(Qt::ISODateWithMs); }

Message Message::fromJson(const QJsonObject &o)
{
    Message m;
    m.id = o[u"id"].toString();
    m.conversationId = o[u"conversation_id"].toString();
    m.seq = o[u"seq"].toInteger();
    m.senderId = o[u"sender_id"].toString();
    m.clientId = o[u"client_id"].toString();
    m.kind = o[u"kind"].toString();
    m.body = o[u"body"].toString();
    m.meta = o[u"meta"].toObject();
    m.createdAt = ts(o[u"created_at"]);
    return m;
}

QJsonObject Message::toJson() const
{
    return {{QStringLiteral("id"), id}, {QStringLiteral("conversation_id"), conversationId}, {QStringLiteral("seq"), seq}, {QStringLiteral("sender_id"), senderId},
            {QStringLiteral("client_id"), clientId}, {QStringLiteral("kind"), kind}, {QStringLiteral("body"), body}, {QStringLiteral("meta"), meta}, {QStringLiteral("created_at"), iso(createdAt)}};
}

Member Member::fromJson(const QJsonObject &o)
{
    return {o[u"user_id"].toString(), o[u"username"].toString(), o[u"display_name"].toString(), o[u"role"].toString(),
            o[u"last_read_seq"].toInteger(), o[u"last_delivered_seq"].toInteger()};
}

QJsonObject Member::toJson() const
{
    return {{QStringLiteral("user_id"), userId}, {QStringLiteral("username"), username}, {QStringLiteral("display_name"), displayName}, {QStringLiteral("role"), role},
            {QStringLiteral("last_read_seq"), lastReadSeq}, {QStringLiteral("last_delivered_seq"), lastDeliveredSeq}};
}

Conversation Conversation::fromJson(const QJsonObject &o)
{
    Conversation c;
    c.id = o[u"id"].toString();
    c.kind = o[u"kind"].toString();
    c.title = o[u"title"].toString();
    c.createdAt = ts(o[u"created_at"]);
    c.lastActivityAt = ts(o[u"last_activity_at"]);
    c.lastSeq = o[u"last_seq"].toInteger();
    c.lastReadSeq = o[u"last_read_seq"].toInteger();
    c.unread = o[u"unread"].toInteger();
    c.muted = o[u"muted"].toBool();
    c.archived = o[u"archived"].toBool();
    c.left = o[u"left"].toBool();
    if (o[u"last_message"].isObject())
        c.lastMessage = Message::fromJson(o[u"last_message"].toObject());
    for (const auto &m : o[u"members"].toArray())
        c.members.append(Member::fromJson(m.toObject()));
    return c;
}

QJsonObject Conversation::toJson() const
{
    QJsonArray ms;
    for (const auto &m : members)
        ms.append(m.toJson());
    return {{QStringLiteral("id"), id}, {QStringLiteral("kind"), kind}, {QStringLiteral("title"), title.isEmpty() ? QJsonValue() : QJsonValue(title)},
            {QStringLiteral("created_at"), iso(createdAt)}, {QStringLiteral("last_seq"), lastSeq}, {QStringLiteral("last_activity_at"), iso(lastActivityAt)},
            {QStringLiteral("last_read_seq"), lastReadSeq}, {QStringLiteral("unread"), unread}, {QStringLiteral("muted"), muted}, {QStringLiteral("archived"), archived},
            {QStringLiteral("left"), left}, {QStringLiteral("last_message"), lastMessage ? QJsonValue(lastMessage->toJson()) : QJsonValue()},
            {QStringLiteral("members"), ms}};
}
