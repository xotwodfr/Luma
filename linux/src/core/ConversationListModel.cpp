#include "ConversationListModel.h"

#include "Format.h"
#include "Store.h"
#include "Sync.h"

ConversationListModel::ConversationListModel(Store &store, Sync &sync, QObject *parent)
    : QAbstractListModel(parent)
    , m_store(store)
    , m_sync(sync)
{
    connect(&m_sync, &Sync::conversationsChanged, this, &ConversationListModel::refresh);
    connect(&m_sync, &Sync::typingChanged, this, &ConversationListModel::refresh);
    connect(&m_sync, &Sync::presenceChanged, this, &ConversationListModel::refresh);
}

QString ConversationListModel::titleFor(const Conversation &c, const QString &me)
{
    if (!c.title.isEmpty())
        return c.title;
    QStringList others;
    for (const auto &m : c.members)
        if (m.userId != me)
            others << m.displayName;
    return others.isEmpty() ? u"Only you"_s : others.join(u", "_s);
}

void ConversationListModel::setFilter(const QString &f)
{
    if (f == m_filter)
        return;
    m_filter = f;
    emit filterChanged();
    refresh();
}

void ConversationListModel::setSelected(const QString &id)
{
    if (id == m_selected)
        return;
    m_selected = id;
    emit selectedChanged();
    if (!m_rows.isEmpty())
        emit dataChanged(index(0), index(int(m_rows.size() - 1)), {SelectedRole, UnreadRole});
}

void ConversationListModel::refresh()
{
    QList<Conversation> next;
    for (const auto &c : m_store.conversations())
        if (m_filter.isEmpty() || titleFor(c, m_sync.me()).contains(m_filter.trimmed(), Qt::CaseInsensitive))
            next.append(c);
    beginResetModel();
    m_rows = next;
    endResetModel();
    emit countChanged();
}

int ConversationListModel::rowCount(const QModelIndex &parent) const { return parent.isValid() ? 0 : int(m_rows.size()); }

QVariant ConversationListModel::data(const QModelIndex &index, int role) const
{
    if (!index.isValid() || index.row() >= m_rows.size())
        return {};
    const auto &c = m_rows[index.row()];
    const QString me = m_sync.me();
    const QString title = titleFor(c, me);
    const bool typing = !m_sync.typingUsers(c.id).isEmpty();
    const bool selected = c.id == m_selected;
    const qint64 unread = selected ? 0 : c.unread;
    auto otherOnline = [&] {
        if (c.kind != u"direct")
            return false;
        for (const auto &m : c.members)
            if (m.userId != me)
                return m_sync.isOnline(m.userId);
        return false;
    };
    auto preview = [&] {
        if (typing)
            return u"typing…"_s;
        if (!c.lastMessage)
            return QString();
        const QString body = c.lastMessage->body.simplified();
        if (c.lastMessage->senderId == me)
            return u"You: "_s + body;
        if (c.kind == u"group")
            for (const auto &m : c.members)
                if (m.userId == c.lastMessage->senderId)
                    return m.displayName.section(u' ', 0, 0) + u": "_s + body;
        return body;
    };
    switch (role) {
    case IdRole: return c.id;
    case TitleRole: return title;
    case InitialsRole: return fmt::initials(title);
    case PairInitialsRole: {
        QStringList out;
        for (const auto &m : c.members)
            if (m.userId != me && out.size() < 2)
                out << fmt::initials(m.displayName).left(1);
        return out;
    }
    case PreviewRole: return preview();
    case TimeRole: return fmt::listTime(c.lastMessage ? c.lastMessage->createdAt : c.lastActivityAt, QDateTime::currentDateTime());
    case UnreadRole: return unread;
    case MutedRole: return c.muted;
    case OnlineRole: return otherOnline();
    case TypingRole: return typing;
    case SelectedRole: return selected;
    case KindRole: return c.kind;
    case AccessibleRole:
        return u"%1%2, %3, %4"_s.arg(title, unread ? u", %1 unread"_s.arg(unread) : QString(),
                                       typing ? u"typing"_s : u"last message: "_s + preview(),
                                       fmt::listTime(c.lastActivityAt, QDateTime::currentDateTime()));
    }
    return {};
}

QHash<int, QByteArray> ConversationListModel::roleNames() const
{
    return {{IdRole, "conversationId"}, {TitleRole, "title"}, {InitialsRole, "initials"}, {PairInitialsRole, "pairInitials"},
            {PreviewRole, "preview"}, {TimeRole, "time"}, {UnreadRole, "unread"}, {MutedRole, "muted"},
            {OnlineRole, "online"}, {TypingRole, "typing"}, {SelectedRole, "selected"}, {KindRole, "kind"},
            {AccessibleRole, "accessibleText"}};
}
