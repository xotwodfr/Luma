#include "MessageListModel.h"

#include "Format.h"
#include "Store.h"
#include "Sync.h"

static constexpr qint64 kGroupGapSecs = 180;

MessageListModel::MessageListModel(Store &store, Sync &sync, QObject *parent)
    : QAbstractListModel(parent)
    , m_store(store)
    , m_sync(sync)
{
    connect(&m_sync, &Sync::messagesChanged, this, [this](const QString &conv) {
        if (conv == m_conversation)
            refresh();
    });
}

void MessageListModel::setConversation(const QString &id)
{
    if (id == m_conversation)
        return;
    beginResetModel();
    m_conversation = id;
    m_rows.clear();
    endResetModel();
    refresh();
    // Always fetch the latest page on open: the cache may hold only the list's last_message,
    // and a partial cache must never pass for the full history. Upserts are idempotent.
    if (!id.isEmpty())
        m_sync.ensureHistory(id);
}

QList<MessageRow> MessageListModel::buildRows(const QList<Message> &messages, const QList<OutboxItem> &pending,
                                              const Conversation &conv, const QString &me, const QDateTime &now)
{
    // Receipts for my messages come from the other members' watermarks (the lowest one wins).
    qint64 othersRead = std::numeric_limits<qint64>::max(), othersDelivered = othersRead;
    QHash<QString, QString> names;
    for (const auto &mem : conv.members) {
        names[mem.userId] = mem.displayName;
        if (mem.userId != me) {
            othersRead = std::min(othersRead, mem.lastReadSeq);
            othersDelivered = std::min(othersDelivered, std::max(mem.lastDeliveredSeq, mem.lastReadSeq));
        }
    }
    if (othersRead == std::numeric_limits<qint64>::max())
        othersRead = othersDelivered = 0;

    QList<MessageRow> rows;
    for (const auto &msg : messages) {
        MessageRow r;
        r.mine = msg.senderId == me;
        r.key = r.mine && !msg.clientId.isEmpty() ? msg.clientId : msg.id;
        r.id = msg.id;
        r.clientId = msg.clientId;
        r.body = msg.body;
        r.senderId = msg.senderId;
        r.senderName = names.value(msg.senderId);
        r.kind = msg.kind;
        r.seq = msg.seq;
        r.at = msg.createdAt.toLocalTime();
        if (r.mine)
            r.receipt = msg.seq <= othersRead ? u"read"_s : msg.seq <= othersDelivered ? u"delivered"_s : u"sent"_s;
        rows.append(r);
    }
    for (const auto &item : pending) {
        MessageRow r;
        r.mine = true;
        r.key = r.clientId = item.clientId.toString(QUuid::WithoutBraces);
        r.body = item.body;
        r.senderId = me;
        r.kind = u"text"_s;
        r.at = item.createdAt.toLocalTime();
        r.receipt = item.state == OutboxItem::Failed ? u"failed"_s : u"pending"_s;
        rows.append(r);
    }

    const QDate today = now.toLocalTime().date();
    const bool group = conv.kind == u"group";
    auto joins = [&](const MessageRow &a, const MessageRow &b) {
        return a.senderId == b.senderId && a.kind != u"system" && b.kind != u"system"
            && a.at.date() == b.at.date() && a.at.secsTo(b.at) <= kGroupGapSecs;
    };
    for (qsizetype i = 0; i < rows.size(); ++i) {
        auto &r = rows[i];
        const bool prev = i > 0 && joins(rows[i - 1], r);
        const bool next = i + 1 < rows.size() && joins(r, rows[i + 1]);
        r.groupPos = prev && next ? u"middle"_s : prev ? u"last"_s : next ? u"first"_s : u"single"_s;
        r.showDate = i == 0 || rows[i - 1].at.date() != r.at.date();
        r.dateLabel = fmt::dayLabel(r.at.date(), today);
        r.time = fmt::timeOfDay(r.at);
        r.showSender = group && !r.mine && !prev;
    }
    return rows;
}

void MessageListModel::refresh()
{
    QList<MessageRow> next;
    if (!m_conversation.isEmpty()) {
        const auto conv = m_store.conversation(m_conversation).value_or(Conversation{});
        next = buildRows(m_store.messages(m_conversation, 300), m_sync.pending(m_conversation), conv, m_sync.me(),
                         QDateTime::currentDateTime());
    }
    // Incremental update when the old rows are a prefix (by key) of the new ones: the common
    // case of new or confirmed messages, so the view keeps its scroll position.
    bool prefix = next.size() >= m_rows.size();
    for (qsizetype i = 0; prefix && i < m_rows.size(); ++i)
        prefix = m_rows[i].key == next[i].key;
    if (!prefix) {
        beginResetModel();
        m_rows = next;
        endResetModel();
        emit countChanged();
        return;
    }
    for (qsizetype i = 0; i < m_rows.size(); ++i) {
        if (!(m_rows[i] == next[i])) {
            m_rows[i] = next[i];
            emit dataChanged(index(int(i)), index(int(i)));
        }
    }
    if (next.size() > m_rows.size()) {
        beginInsertRows({}, int(m_rows.size()), int(next.size() - 1));
        m_rows = next;
        endInsertRows();
        emit countChanged();
    }
}

int MessageListModel::rowCount(const QModelIndex &parent) const { return parent.isValid() ? 0 : int(m_rows.size()); }

QVariant MessageListModel::data(const QModelIndex &index, int role) const
{
    if (!index.isValid() || index.row() >= m_rows.size())
        return {};
    const auto &r = m_rows[index.row()];
    switch (role) {
    case KeyRole: return r.key;
    case IdRole: return r.id;
    case ClientIdRole: return r.clientId;
    case BodyRole: return r.body;
    case SenderNameRole: return r.senderName;
    case MineRole: return r.mine;
    case TimeRole: return r.time;
    case ReceiptRole: return r.receipt;
    case GroupPosRole: return r.groupPos;
    case ShowDateRole: return r.showDate;
    case DateLabelRole: return r.dateLabel;
    case ShowSenderRole: return r.showSender;
    case KindRole: return r.kind;
    case SeqRole: return r.seq;
    }
    return {};
}

QHash<int, QByteArray> MessageListModel::roleNames() const
{
    return {{KeyRole, "key"}, {IdRole, "messageId"}, {ClientIdRole, "clientId"}, {BodyRole, "body"},
            {SenderNameRole, "senderName"}, {MineRole, "mine"}, {TimeRole, "time"}, {ReceiptRole, "receipt"},
            {GroupPosRole, "groupPos"}, {ShowDateRole, "showDate"}, {DateLabelRole, "dateLabel"},
            {ShowSenderRole, "showSender"}, {KindRole, "kind"}, {SeqRole, "seq"}};
}
