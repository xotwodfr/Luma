#include "RealtimeClient.h"

#include <QJsonDocument>
#include <QNetworkInformation>
#include <QNetworkRequest>

using namespace std::chrono_literals;

static constexpr auto kPingEvery = 25s;
static constexpr auto kDeadAfter = 60s;

RealtimeClient::RealtimeClient(QUrl url, QObject *parent)
    : Transport(parent)
    , m_url(std::move(url))
{
    m_retry.setSingleShot(true);
    connect(&m_retry, &QTimer::timeout, this, &RealtimeClient::open);
    connect(&m_ws, &QWebSocket::connected, this, [this] {
        m_lastFrame.start();
        QJsonObject hello;
        hello[u"cursor"] = m_cursor ? QJsonValue(*m_cursor) : QJsonValue(QJsonValue::Null);
        sendFrame({{QStringLiteral("t"), QStringLiteral("hello")}, {QStringLiteral("d"), hello}});
        m_ping.start(kPingEvery);
    });
    connect(&m_ws, &QWebSocket::textMessageReceived, this, &RealtimeClient::onText);
    connect(&m_ws, &QWebSocket::disconnected, this, &RealtimeClient::onDisconnected);
    connect(&m_ping, &QTimer::timeout, this, [this] {
        if (m_lastFrame.durationElapsed() > kDeadAfter) {
            m_ws.abort(); // silent half-open socket: reconnect
            return;
        }
        sendFrame({{QStringLiteral("t"), QStringLiteral("ping")}});
    });

    if (QNetworkInformation::loadBackendByFeatures(QNetworkInformation::Feature::Reachability)) {
        connect(QNetworkInformation::instance(), &QNetworkInformation::reachabilityChanged, this, [this](auto r) {
            if (!m_running)
                return;
            if (r == QNetworkInformation::Reachability::Disconnected) {
                setState(State::Offline);
            } else if (state() == State::Offline || state() == State::Reconnecting) {
                m_retry.stop();
                m_backoff.reset();
                open(); // the network came back: retry now
            }
        });
    }
}

void RealtimeClient::start(const QString &token, std::optional<qint64> cursor)
{
    m_token = token;
    m_cursor = cursor;
    m_running = true;
    m_backoff.reset();
    open();
}

void RealtimeClient::stop()
{
    m_running = false;
    m_retry.stop();
    m_ping.stop();
    m_ws.close();
    setState(State::Idle);
}

void RealtimeClient::open()
{
    if (!m_running)
        return;
    if (cursorProvider)
        m_cursor = cursorProvider();
    setState(state() == State::Reconnecting || state() == State::Offline ? State::Reconnecting : State::Connecting);
    QNetworkRequest req(m_url);
    req.setRawHeader("Authorization", "Bearer " + m_token.toUtf8());
    m_ws.open(req);
}

void RealtimeClient::sendFrame(const QJsonObject &frame)
{
    if (m_ws.state() == QAbstractSocket::ConnectedState)
        m_ws.sendTextMessage(QString::fromUtf8(QJsonDocument(frame).toJson(QJsonDocument::Compact)));
}

int RealtimeClient::retryInSeconds() const
{
    return m_retry.isActive() ? (m_retry.remainingTime() + 999) / 1000 : 0;
}

void RealtimeClient::onText(const QString &text)
{
    m_lastFrame.start();
    const QJsonObject f = QJsonDocument::fromJson(text.toUtf8()).object();
    if (f[u"t"].toString() == u"ready") {
        m_backoff.reset();
        setState(State::Online);
    }
    emit frame(f);
}

void RealtimeClient::onDisconnected()
{
    m_ping.stop();
    const int code = int(m_ws.closeCode());
    if (code == 4001 || code == 4003) {
        m_running = false;
        setState(State::Idle);
        emit closed(code);
        return;
    }
    scheduleReconnect();
}

void RealtimeClient::scheduleReconnect()
{
    if (!m_running)
        return;
    const bool offline = QNetworkInformation::instance()
        && QNetworkInformation::instance()->reachability() == QNetworkInformation::Reachability::Disconnected;
    m_retry.start(m_backoff.next());
    setState(offline ? State::Offline : State::Reconnecting);
}
