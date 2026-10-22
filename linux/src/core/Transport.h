#pragma once

#include <QJsonObject>
#include <QObject>
#include <optional>

// The realtime connection as Sync sees it. RealtimeClient is the real one; tests inject frames.
class Transport : public QObject {
    Q_OBJECT
public:
    enum class State { Idle, Connecting, Online, Reconnecting, Offline };
    Q_ENUM(State)

    using QObject::QObject;

    // Open the socket and send hello with this cursor (nullopt = fresh client).
    virtual void start(const QString &token, std::optional<qint64> cursor) = 0;
    virtual void stop() = 0;
    virtual void sendFrame(const QJsonObject &frame) = 0;

    State state() const { return m_state; }
    // Seconds until the next reconnect attempt while Reconnecting.
    virtual int retryInSeconds() const { return 0; }

    // Asked before every reconnect, so replay resumes from the latest applied event.
    std::function<std::optional<qint64>()> cursorProvider;

signals:
    void frame(const QJsonObject &frame);
    void stateChanged();
    // The server closed with a terminal code (4001 unauthorized, 4003 revoked): don't reconnect.
    void closed(int code);

protected:
    void setState(State s)
    {
        if (s != m_state) {
            m_state = s;
            emit stateChanged();
        }
    }

private:
    State m_state = State::Idle;
};
