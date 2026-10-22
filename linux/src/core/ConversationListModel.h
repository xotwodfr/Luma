#pragma once

#include <QAbstractListModel>

#include "Types.h"

class Store;
class Sync;

// The conversation list (DESIGN.md §7 "Conversation row"), filtered by a local search string.
class ConversationListModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(QString filter READ filter WRITE setFilter NOTIFY filterChanged)
    Q_PROPERTY(QString selected READ selected WRITE setSelected NOTIFY selectedChanged)
public:
    enum Role { IdRole = Qt::UserRole + 1, TitleRole, InitialsRole, PairInitialsRole, PreviewRole, TimeRole,
                UnreadRole, MutedRole, OnlineRole, TypingRole, SelectedRole, KindRole, AccessibleRole };

    ConversationListModel(Store &store, Sync &sync, QObject *parent = nullptr);

    QString filter() const { return m_filter; }
    void setFilter(const QString &f);
    QString selected() const { return m_selected; }
    void setSelected(const QString &id);
    void refresh();

    // The display title of a conversation as the current user sees it.
    static QString titleFor(const Conversation &c, const QString &me);

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;

signals:
    void countChanged();
    void filterChanged();
    void selectedChanged();

private:
    Store &m_store;
    Sync &m_sync;
    QString m_filter, m_selected;
    QList<Conversation> m_rows;
};
