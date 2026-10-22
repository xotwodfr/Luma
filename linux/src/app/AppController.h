#pragma once

#include <QObject>
#include <QTimer>
#include <QVariantMap>
#include <memory>

#include "ApiClient.h"
#include "ConversationListModel.h"
#include "MessageListModel.h"
#include "RealtimeClient.h"
#include "Store.h"
#include "Sync.h"

// Everything the QML interface binds to, exposed as the `App` singleton.
class AppController : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString phase READ phase NOTIFY phaseChanged)                  // signedOut | signingIn | ready
    Q_PROPERTY(QString connection READ connection NOTIFY connectionChanged)   // online | connecting | reconnecting | offline
    Q_PROPERTY(int retryIn READ retryIn NOTIFY connectionChanged)
    Q_PROPERTY(bool synced READ synced NOTIFY syncedChanged)                  // first conversation list has arrived
    Q_PROPERTY(QString serverHost READ serverHost CONSTANT)
    Q_PROPERTY(QVariantMap me READ me NOTIFY meChanged)
    Q_PROPERTY(QString errorText READ errorText NOTIFY errorTextChanged)
    Q_PROPERTY(QString notice READ notice NOTIFY noticeChanged)
    Q_PROPERTY(ConversationListModel *conversations READ conversations CONSTANT)
    Q_PROPERTY(MessageListModel *messages READ messages CONSTANT)
    Q_PROPERTY(QString current READ current NOTIFY currentChanged)
    Q_PROPERTY(QVariantMap header READ header NOTIFY headerChanged)

public:
    AppController(const QUrl &server, const QString &profileDir, QObject *parent = nullptr);
    ~AppController() override;

    QString phase() const { return m_phase; }
    QString connection() const;
    int retryIn() const { return m_transport->retryInSeconds(); }
    bool synced() const { return m_synced; }
    QString serverHost() const { return m_api->base().host(); }
    QVariantMap me() const { return m_me; }
    QString errorText() const { return m_error; }
    QString notice() const { return m_notice; }
    ConversationListModel *conversations() const { return m_conversations.get(); }
    MessageListModel *messages() const { return m_messages.get(); }
    QString current() const { return m_current; }
    QVariantMap header() const;

    Q_INVOKABLE void restoreSession();
    Q_INVOKABLE void signIn(const QString &username, const QString &password);
    Q_INVOKABLE void signUp(const QString &invite, const QString &username, const QString &displayName, const QString &password);
    Q_INVOKABLE void signOut();
    Q_INVOKABLE void openConversation(const QString &id);
    Q_INVOKABLE void startDirect(const QString &username);
    Q_INVOKABLE void send(const QString &text);
    Q_INVOKABLE void retry(const QString &clientId);
    Q_INVOKABLE void markVisibleRead();
    Q_INVOKABLE void composerEdited();
    Q_INVOKABLE void clearError() { setError({}); }

signals:
    void phaseChanged();
    void connectionChanged();
    void syncedChanged();
    void meChanged();
    void errorTextChanged();
    void noticeChanged();
    void currentChanged();
    void headerChanged();
    void startDirectFinished(bool ok, const QString &message);

private:
    void setPhase(const QString &p);
    void setError(const QString &e);
    void begin(const QJsonObject &authResponse);
    void startSession(const QString &token, const QVariantMap &me);
    void endSession(const QString &notice);
    void saveSession(const QString &token);
    void forgetSession();
    QString sessionKey() const;

    std::unique_ptr<Store> m_store;
    std::unique_ptr<ApiClient> m_api;
    std::unique_ptr<RealtimeClient> m_transport;
    std::unique_ptr<Sync> m_sync;
    std::unique_ptr<ConversationListModel> m_conversations;
    std::unique_ptr<MessageListModel> m_messages;
    QTimer m_tick;
    QString m_phase = QStringLiteral("signedOut"), m_error, m_notice, m_current;
    QVariantMap m_me;
    bool m_synced = false;
    quint64 m_gen = 0; // session generation: replies from a previous session are dropped
};
