#include "ApiClient.h"

#include <QNetworkReply>
#include <QSysInfo>
#include <QUrlQuery>

ApiClient::ApiClient(QUrl base, QObject *parent)
    : QObject(parent)
    , m_base(std::move(base))
{
    m_nam.setTransferTimeout(15000);
}

QUrl ApiClient::realtimeUrl() const
{
    QUrl u = m_base;
    u.setScheme(m_base.scheme() == u"https" ? QStringLiteral("wss") : QStringLiteral("ws"));
    u.setPath(QStringLiteral("/v1/realtime"));
    return u;
}

void ApiClient::send(const QByteArray &verb, const QString &path, const QJsonObject *body, Callback cb)
{
    QUrl url = m_base;
    const QString p = path.section(u'?', 0, 0);
    url.setPath(p);
    if (path.contains(u'?'))
        url.setQuery(path.section(u'?', 1));
    QNetworkRequest req(url);
    req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));
    req.setRawHeader("Accept", "application/json");
    const bool authed = !m_token.isEmpty();
    if (authed)
        req.setRawHeader("Authorization", "Bearer " + m_token.toUtf8());

    QNetworkReply *reply = m_nam.sendCustomRequest(req, verb, body ? QJsonDocument(*body).toJson(QJsonDocument::Compact) : QByteArray());
    connect(reply, &QNetworkReply::finished, this, [this, reply, cb = std::move(cb), authed] {
        reply->deleteLater();
        ApiResult r;
        r.status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        r.body = QJsonDocument::fromJson(reply->readAll());
        const QJsonObject err = r.body.object().value(u"error").toObject();
        r.errorCode = err.value(u"code").toString();
        r.errorMessage = err.value(u"message").toString();
        if (r.status == 0) {
            r.errorCode = QStringLiteral("network");
            r.errorMessage = tr("Can't reach the server. Check your connection.");
        }
        if (r.status == 401 && authed)
            emit unauthorized();
        if (cb)
            cb(r);
    });
}

static QJsonObject device()
{
    return {{QStringLiteral("device_name"), QSysInfo::machineHostName().left(64)}, {QStringLiteral("platform"), QStringLiteral("linux")}};
}

void ApiClient::login(const QString &username, const QString &password, Callback cb)
{
    QJsonObject b = device();
    b[u"username"] = username;
    b[u"password"] = password;
    send("POST", QStringLiteral("/v1/auth/login"), &b, std::move(cb));
}

void ApiClient::registerAccount(const QString &invite, const QString &username, const QString &displayName, const QString &password, Callback cb)
{
    QJsonObject b = device();
    b[u"invite"] = invite;
    b[u"username"] = username;
    b[u"display_name"] = displayName;
    b[u"password"] = password;
    send("POST", QStringLiteral("/v1/auth/register"), &b, std::move(cb));
}

void ApiClient::logout(Callback cb) { send("POST", QStringLiteral("/v1/auth/logout"), nullptr, std::move(cb)); }
void ApiClient::me(Callback cb) { send("GET", QStringLiteral("/v1/me"), nullptr, std::move(cb)); }
void ApiClient::conversations(Callback cb) { send("GET", QStringLiteral("/v1/conversations"), nullptr, std::move(cb)); }

void ApiClient::conversation(const QString &id, Callback cb)
{
    send("GET", QStringLiteral("/v1/conversations/") + QString::fromLatin1(QUrl::toPercentEncoding(id)), nullptr, std::move(cb));
}

void ApiClient::openDirect(const QString &username, Callback cb)
{
    const QJsonObject b{{QStringLiteral("kind"), QStringLiteral("direct")}, {QStringLiteral("username"), username}};
    send("POST", QStringLiteral("/v1/conversations"), &b, std::move(cb));
}

void ApiClient::history(const QString &conversationId, qint64 beforeSeq, int limit, Callback cb)
{
    QUrlQuery q;
    if (beforeSeq > 0)
        q.addQueryItem(QStringLiteral("before_seq"), QString::number(beforeSeq));
    q.addQueryItem(QStringLiteral("limit"), QString::number(limit));
    send("GET", QStringLiteral("/v1/conversations/%1/messages?%2").arg(QString::fromLatin1(QUrl::toPercentEncoding(conversationId)), q.toString()),
         nullptr, std::move(cb));
}
