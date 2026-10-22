#pragma once

#include <QElapsedTimer>
#include <QTimer>
#include <QUrl>
#include <QWebSocket>

#include "Backoff.h"
#include "Transport.h"

// WebSocket transport (protocol/realtime.md): Authorization header + hello{cursor}, reconnect with
// full-jitter backoff, 25 s pings, dead-socket detection, immediate retry when the network returns.
class RealtimeClient : public Transport {
    Q_OBJECT
public:
    explicit RealtimeClient(QUrl url, QObject *parent = nullptr);

    void start(const QString &token, std::optional<qint64> cursor) override;
    void stop() override;
    void sendFrame(const QJsonObject &frame) override;
    int retryInSeconds() const override;

private:
    void open();
    void scheduleReconnect();
    void onText(const QString &text);
    void onDisconnected();

    QUrl m_url;
    QString m_token;
    std::optional<qint64> m_cursor;
    QWebSocket m_ws;
    Backoff m_backoff;
    QTimer m_retry, m_ping;
    QElapsedTimer m_lastFrame;
    bool m_running = false;
};
