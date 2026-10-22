#pragma once

#include <QAbstractListModel>

#include "Types.h"

class Sync;
class Store;

struct MessageRow {
    QString key;      // client_id for my messages (pending → confirmed keeps its row), else id
    QString id, clientId, body, senderId, senderName, kind;
    qint64 seq = 0;
    bool mine = false;
    QDateTime at;
    QString time;
    QString receipt;  // pending | sent | delivered | read | failed ("" for others' messages)
    QString groupPos; // single | first | middle | last
    bool showDate = false;
    QString dateLabel;
    bool showSender = false;

    bool operator==(const MessageRow &) const = default;
};

// Messages of the open conversation plus its pending outbox items, grouped per DESIGN.md §4.
class MessageListModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
public:
    enum Role { KeyRole = Qt::UserRole + 1, IdRole, ClientIdRole, BodyRole, SenderNameRole, MineRole, TimeRole,
                ReceiptRole, GroupPosRole, ShowDateRole, DateLabelRole, ShowSenderRole, KindRole, SeqRole };

    MessageListModel(Store &store, Sync &sync, QObject *parent = nullptr);

    void setConversation(const QString &id);
    QString conversation() const { return m_conversation; }
    void refresh();

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;

    static QList<MessageRow> buildRows(const QList<Message> &messages, const QList<OutboxItem> &pending,
                                       const Conversation &conv, const QString &me, const QDateTime &now);

signals:
    void countChanged();

private:
    Store &m_store;
    Sync &m_sync;
    QString m_conversation;
    QList<MessageRow> m_rows;
};
