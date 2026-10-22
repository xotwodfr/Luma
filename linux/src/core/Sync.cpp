#include "Sync.h"

#include <QJsonArray>

static QJsonObject frame(const QString &t, const QString &id, const QJsonObject &d)
{
    QJsonObject f{{QStringLiteral("t"), t}, {QStringLiteral("d"), d}};
    if (!id.isEmpty())
        f[u"id"] = id;
    return f;
}

static qint64 nowMs() { return QDateTime::currentMSecsSinceEpoch(); }

Sync::Sync(Store &store, ApiClient &api, Transport &transport, QObject *parent)
    : QObject(parent)
    , m_store(store)
    , m_api(api)
    , m_transport(transport)
{
    connect(&m_transport, &Transport::frame, this, &Sync::onFrame);
    connect(&m_transport, &Transport::closed, this, [this](int code) {
        signOut(code == 4003 ? QStringLiteral("revoked") : QStringLiteral("unauthorized"));
    });
    connect(&m_transport, &Transport::stateChanged, this, [this] {
        if (m_transport.state() != Transport::State::Online && m_ready) {
            m_ready = false;
            emit readyChanged();
        }
        // A failed (re)connect may mean the token was revoked while we were away: the handshake is
        // refused with HTTP 401, which WebSockets can't report distinctly. Ask REST, throttled.
        if (m_transport.state() == Transport::State::Reconnecting && nowMs() - m_lastSessionCheck >= sessionCheckEveryMs) {
            m_lastSessionCheck = nowMs();
            m_api.me([](const ApiResult &) {}); // a 401 emits ApiClient::unauthorized
        }
    });
    connect(&m_api, &ApiClient::unauthorized, this, [this] { signOut(QStringLiteral("unauthorized")); });
    m_transport.cursorProvider = [this] { return m_store.cursor(); };
    m_deliveredTimer.setSingleShot(true);
    m_deliveredTimer.setInterval(300);
    connect(&m_deliveredTimer, &QTimer::timeout, this, &Sync::flushDelivered);
}

void Sync::start(const QString &token, const QString &myUserId)
{
    ++m_gen;
    m_token = token;
    m_me = myUserId;
    if (m_store.cursor().has_value())
        reloadConversations(); // refresh the list; cursor-based events keep it current afterwards
    m_transport.start(token, m_store.cursor());
}

void Sync::signOut(const QString &reason)
{
    ++m_gen;
    m_transport.stop();
    m_ready = false;
    emit signedOut(reason);
}

void Sync::stop()
{
    ++m_gen;
    m_transport.stop();
    m_ready = false;
}

QUuid Sync::send(const QString &conversationId, const QString &text)
{
    if (text.trimmed().isEmpty() || text.size() > 8000)
        return {};
    OutboxItem item{QUuid::createUuid(), conversationId, text, QDateTime::currentDateTimeUtc(), OutboxItem::Pending};
    m_store.addOutbox(item); // durable before anything goes on the wire
    if (m_ready)
        sendItem(item);
    emit messagesChanged(conversationId);
    return item.clientId;
}

void Sync::sendItem(const OutboxItem &item)
{
    const QString cid = item.clientId.toString(QUuid::WithoutBraces);
    m_transport.sendFrame(frame(QStringLiteral("send"), cid,
                                {{QStringLiteral("conversation_id"), item.conversationId},
                                 {QStringLiteral("client_id"), cid},
                                 {QStringLiteral("kind"), QStringLiteral("text")},
                                 {QStringLiteral("body"), item.body}}));
}

void Sync::retry(const QUuid &clientId)
{
    for (const auto &item : m_store.outbox()) {
        if (item.clientId != clientId)
            continue;
        m_store.setOutboxState(clientId, OutboxItem::Pending);
        if (m_ready)
            sendItem(item);
        emit messagesChanged(item.conversationId);
    }
}

void Sync::flushOutbox()
{
    for (const auto &item : m_store.outbox())
        if (item.state == OutboxItem::Pending)
            sendItem(item);
    for (auto it = m_pendingReads.begin(); it != m_pendingReads.end(); ++it)
        m_transport.sendFrame(frame(QStringLiteral("read"), {}, {{QStringLiteral("conversation_id"), it.key()}, {QStringLiteral("seq"), it.value()}}));
    m_pendingReads.clear();
}

QList<OutboxItem> Sync::pending(const QString &conversationId) const
{
    QList<OutboxItem> out;
    for (const auto &item : m_store.outbox())
        if (item.conversationId == conversationId)
            out.append(item);
    return out;
}

void Sync::markRead(const QString &conversationId)
{
    auto c = m_store.conversation(conversationId);
    if (!c || c->lastSeq <= c->lastReadSeq)
        return;
    c->lastReadSeq = c->lastSeq;
    c->unread = 0;
    m_store.upsertConversation(*c);
    emit conversationsChanged();
    if (m_ready)
        m_transport.sendFrame(frame(QStringLiteral("read"), {}, {{QStringLiteral("conversation_id"), conversationId}, {QStringLiteral("seq"), c->lastSeq}}));
    else
        m_pendingReads[conversationId] = c->lastSeq;
}

void Sync::typing(const QString &conversationId)
{
    if (!m_ready || nowMs() - m_lastTypingSent.value(conversationId) < 3000)
        return;
    m_lastTypingSent[conversationId] = nowMs();
    m_transport.sendFrame(frame(QStringLiteral("typing"), {}, {{QStringLiteral("conversation_id"), conversationId}}));
}

QStringList Sync::typingUsers(const QString &conversationId) const
{
    QStringList out;
    const auto users = m_typing.value(conversationId);
    for (auto it = users.begin(); it != users.end(); ++it)
        if (it.value() > nowMs())
            out.append(it.key());
    return out;
}

void Sync::onFrame(const QJsonObject &f)
{
    const QString t = f[u"t"].toString();
    const QJsonObject d = f[u"d"].toObject();
    if (t == u"event") {
        applyEvent(d[u"seq"].toInteger(), d[u"type"].toString(), d[u"data"].toObject());
    } else if (t == u"ready") {
        m_ready = true;
        flushOutbox();
        emit readyChanged();
    } else if (t == u"resync_required") {
        m_store.setCursor(d[u"cursor"].toInteger());
        reloadConversations();
    } else if (t == u"ack") {
        if (d[u"message"].isObject())
            applyMessage(Message::fromJson(d[u"message"].toObject()));
    } else if (t == u"error") {
        const QUuid id = QUuid::fromString(f[u"id"].toString());
        if (id.isNull())
            return;
        const QString code = d[u"code"].toString();
        for (const auto &item : m_store.outbox()) {
            if (item.clientId != id)
                continue;
            if (code == u"rate_limited") {
                QTimer::singleShot(5000, this, [this, item, gen = m_gen] { if (gen == m_gen && m_ready) sendItem(item); });
            } else {
                m_store.setOutboxState(id, OutboxItem::Failed);
                emit messagesChanged(item.conversationId);
            }
        }
    } else if (t == u"typing") {
        const QString conv = d[u"conversation_id"].toString();
        m_typing[conv][d[u"user_id"].toString()] = nowMs() + typingTtlMs;
        emit typingChanged(conv);
        QTimer::singleShot(typingTtlMs + 20, this, [this, conv] { emit typingChanged(conv); });
    } else if (t == u"presence") {
        const QString user = d[u"user_id"].toString();
        m_online[user] = d[u"online"].toBool();
        emit presenceChanged(user);
    }
}

void Sync::applyEvent(qint64 seq, const QString &type, const QJsonObject &data)
{
    if (seq <= m_store.cursor().value_or(0))
        return; // already applied (replay overlap)
    const QString convId = data[u"conversation_id"].toString();
    if (type == u"message.created") {
        applyMessage(Message::fromJson(data[u"message"].toObject()));
    } else if (type == u"conversation.created") {
        fetchConversation(convId);
    } else if (type == u"read.updated" || type == u"receipt.updated") {
        if (auto c = m_store.conversation(convId)) {
            const QString user = data[u"user_id"].toString();
            for (auto &m : c->members) {
                if (m.userId == user) {
                    m.lastReadSeq = std::max(m.lastReadSeq, data[u"last_read_seq"].toInteger());
                    m.lastDeliveredSeq = std::max(m.lastDeliveredSeq, data[u"last_delivered_seq"].toInteger());
                }
            }
            if (user == m_me) {
                c->lastReadSeq = std::max(c->lastReadSeq, data[u"last_read_seq"].toInteger());
                c->unread = std::max<qint64>(0, c->lastSeq - c->lastReadSeq);
            }
            m_store.upsertConversation(*c);
            emit conversationsChanged();
            emit messagesChanged(convId);
        }
    }
    m_store.setCursor(seq); // only after the change is in the store
}

void Sync::applyMessage(const Message &m)
{
    m_store.upsertMessage(m);
    const QUuid cid = QUuid::fromString(m.clientId);
    if (m.senderId == m_me && !cid.isNull())
        m_store.removeOutbox(cid); // ack and own event both land here; either one settles the outbox

    auto c = m_store.conversation(m.conversationId);
    if (!c) {
        fetchConversation(m.conversationId);
    } else if (m.seq > c->lastSeq || !c->lastMessage) {
        c->lastSeq = std::max(c->lastSeq, m.seq);
        c->lastMessage = m;
        c->lastActivityAt = m.createdAt;
        if (m.senderId == m_me)
            c->lastReadSeq = m.seq;
        c->unread = std::max<qint64>(0, c->lastSeq - c->lastReadSeq);
        m_store.upsertConversation(*c);
    }
    if (m.senderId != m_me) {
        m_pendingDelivered[m.conversationId] = std::max(m_pendingDelivered.value(m.conversationId), m.seq);
        m_deliveredTimer.start();
    }
    emit messagesChanged(m.conversationId);
    emit conversationsChanged();
}

void Sync::flushDelivered()
{
    if (!m_ready)
        return;
    for (auto it = m_pendingDelivered.begin(); it != m_pendingDelivered.end(); ++it)
        m_transport.sendFrame(frame(QStringLiteral("delivered"), {}, {{QStringLiteral("conversation_id"), it.key()}, {QStringLiteral("seq"), it.value()}}));
    m_pendingDelivered.clear();
}

void Sync::reloadConversations()
{
    m_api.conversations([this, gen = m_gen](const ApiResult &r) {
        if (gen != m_gen || !r.ok())
            return;
        QSet<QString> seen;
        for (const auto &v : r.body.object()[u"conversations"].toArray()) {
            const auto c = Conversation::fromJson(v.toObject());
            seen.insert(c.id);
            m_store.upsertConversation(c);
            if (c.lastMessage)
                m_store.upsertMessage(*c.lastMessage);
        }
        for (const auto &c : m_store.conversations())
            if (!seen.contains(c.id))
                m_store.removeConversation(c.id);
        emit conversationsChanged();
    });
}

void Sync::fetchConversation(const QString &id)
{
    if (id.isEmpty() || m_fetching.contains(id))
        return;
    m_fetching.insert(id);
    m_api.conversation(id, [this, id, gen = m_gen](const ApiResult &r) {
        if (gen != m_gen)
            return;
        m_fetching.remove(id);
        if (!r.ok()) {
            // The message is stored and the cursor has moved on; without a retry this conversation
            // would stay missing from the list until restart. Reload the whole list shortly.
            QTimer::singleShot(retryFetchMs, this, [this, gen] { if (gen == m_gen) reloadConversations(); });
            return;
        }
        const auto c = Conversation::fromJson(r.body.object());
        m_store.upsertConversation(c);
        if (c.lastMessage)
            m_store.upsertMessage(*c.lastMessage);
        emit conversationsChanged();
        emit messagesChanged(id);
    });
}

void Sync::ensureHistory(const QString &conversationId)
{
    m_api.history(conversationId, 0, 50, [this, conversationId, gen = m_gen](const ApiResult &r) {
        if (gen != m_gen || !r.ok())
            return;
        for (const auto &v : r.body.object()[u"messages"].toArray())
            m_store.upsertMessage(Message::fromJson(v.toObject()));
        emit messagesChanged(conversationId);
    });
}
