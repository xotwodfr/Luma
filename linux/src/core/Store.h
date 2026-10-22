#pragma once

#include <QSqlDatabase>

#include "Types.h"

// Local SQLite cache: conversations, messages, the outbox and the realtime cursor.
// Lets the app open instantly and read offline; the outbox makes sends survive restarts.
class Store {
public:
    explicit Store(const QString &path);
    ~Store();
    Store(const Store &) = delete;
    Store &operator=(const Store &) = delete;

    void upsertConversation(const Conversation &c);
    std::optional<Conversation> conversation(const QString &id) const;
    QList<Conversation> conversations() const;
    void removeConversation(const QString &id);

    void upsertMessage(const Message &m);
    QList<Message> messages(const QString &conversationId, int limit) const;

    void addOutbox(const OutboxItem &item);
    void removeOutbox(const QUuid &clientId);
    void setOutboxState(const QUuid &clientId, OutboxItem::State state);
    QList<OutboxItem> outbox() const;

    std::optional<qint64> cursor() const;
    void setCursor(qint64 cursor);

    void clear();

private:
    QSqlDatabase db() const;
    QString m_connection;
};
