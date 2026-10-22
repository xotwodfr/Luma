#pragma once

#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QUrl>
#include <functional>

struct ApiResult {
    int status = 0; // 0 = network error, no HTTP response
    QJsonDocument body;
    QString errorCode, errorMessage;
    bool ok() const { return status >= 200 && status < 300; }
};

// REST client for protocol/openapi.yaml. Callbacks run on the owner's thread.
class ApiClient : public QObject {
    Q_OBJECT
public:
    using Callback = std::function<void(const ApiResult &)>;

    explicit ApiClient(QUrl base, QObject *parent = nullptr);

    QUrl base() const { return m_base; }
    QUrl realtimeUrl() const;
    QString token() const { return m_token; }
    void setToken(const QString &token) { m_token = token; }

    void login(const QString &username, const QString &password, Callback cb);
    void registerAccount(const QString &invite, const QString &username, const QString &displayName, const QString &password, Callback cb);
    void logout(Callback cb);
    void me(Callback cb);
    void conversations(Callback cb);
    void conversation(const QString &id, Callback cb);
    void openDirect(const QString &username, Callback cb);
    void history(const QString &conversationId, qint64 beforeSeq, int limit, Callback cb);

signals:
    // An authenticated request was rejected with 401: the session is gone.
    void unauthorized();

private:
    void send(const QByteArray &verb, const QString &path, const QJsonObject *body, Callback cb);

    QNetworkAccessManager m_nam;
    QUrl m_base;
    QString m_token;
};
