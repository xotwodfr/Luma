#pragma once

#include <QHash>
#include <QSet>
#include <QTimer>

#include "ApiClient.h"
#include "Store.h"
#include "Transport.h"

// Client sync engine: applies realtime frames to the Store, owns the outbox, and keeps the cursor.
// Guarantees (protocol/realtime.md): events applied in order and at most once (by seq), cursor
// persisted only after apply, outbox resent after every ready with the same client_id.
class Sync : public QObject {
    Q_OBJECT
public:
    static inline int typingTtlMs = 5000;
    static inline int sessionCheckEveryMs = 30000;
    static inline int retryFetchMs = 5000;

    Sync(Store &store, ApiClient &api, Transport &transport, QObject *parent = nullptr);

    void start(const QString &token, const QString &myUserId);
    void stop();

    QString me() const { return m_me; }
    bool isReady() const { return m_ready; }

    // Returns the client_id, or a null QUuid if the text is empty or over 8000 characters.
    QUuid send(const QString &conversationId, const QString &text);
    void retry(const QUuid &clientId);
    void markRead(const QString &conversationId);
    void typing(const QString &conversationId);
    // Fetch the latest page from the server when the local cache has nothing for this conversation.
    void ensureHistory(const QString &conversationId);

    QList<OutboxItem> pending(const QString &conversationId) const;
    QStringList typingUsers(const QString &conversationId) const;
    bool isOnline(const QString &userId) const { return m_online.value(userId); }

signals:
    void conversationsChanged();
    void messagesChanged(const QString &conversationId);
    void typingChanged(const QString &conversationId);
    void presenceChanged(const QString &userId);
    void readyChanged();
    void signedOut(const QString &reason);

private:
    void onFrame(const QJsonObject &f);
    void applyEvent(qint64 seq, const QString &type, const QJsonObject &data);
    void applyMessage(const Message &m);
    void flushOutbox();
    void sendItem(const OutboxItem &item);
    void reloadConversations();
    void fetchConversation(const QString &id);
    void flushDelivered();
    void signOut(const QString &reason);

    Store &m_store;
    ApiClient &m_api;
    Transport &m_transport;
    QString m_me, m_token;
    bool m_ready = false;
    QSet<QString> m_fetching;
    QHash<QString, qint64> m_pendingReads, m_pendingDelivered;
    QTimer m_deliveredTimer;
    QHash<QString, QHash<QString, qint64>> m_typing; // conv → user → expiry (ms since epoch)
    QHash<QString, qint64> m_lastTypingSent;
    QHash<QString, bool> m_online;
    qint64 m_lastSessionCheck = 0;
    // Bumped on start/stop; async replies carry the generation they began in and are dropped if
    // it changed, so a late reply can never write one account's data into the next one's store.
    quint64 m_gen = 0;
};
