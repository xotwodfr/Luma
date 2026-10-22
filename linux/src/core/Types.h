#pragma once

// Plain data types mirroring protocol/openapi.yaml (field names match the JSON exactly).

#include <QDateTime>
#include <QJsonArray>
#include <QJsonObject>
#include <QList>
#include <QString>
#include <QUuid>
#include <optional>

struct Message {
    QString id, conversationId, senderId, clientId, kind, body;
    qint64 seq = 0;
    QJsonObject meta;
    QDateTime createdAt;

    static Message fromJson(const QJsonObject &o);
    QJsonObject toJson() const;
};

struct Member {
    QString userId, username, displayName, role;
    qint64 lastReadSeq = 0, lastDeliveredSeq = 0;

    static Member fromJson(const QJsonObject &o);
    QJsonObject toJson() const;
};

struct Conversation {
    QString id, kind, title;
    QDateTime createdAt, lastActivityAt;
    qint64 lastSeq = 0, lastReadSeq = 0, unread = 0;
    bool muted = false, archived = false, left = false;
    std::optional<Message> lastMessage;
    QList<Member> members;

    static Conversation fromJson(const QJsonObject &o);
    QJsonObject toJson() const;
};

struct OutboxItem {
    QUuid clientId;
    QString conversationId, body;
    QDateTime createdAt;
    enum State { Pending, Failed } state = Pending;
};
