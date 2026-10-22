#include "Store.h"

#include <QJsonDocument>
#include <QSqlError>
#include <QSqlQuery>

static QString json(const QJsonObject &o) { return QString::fromUtf8(QJsonDocument(o).toJson(QJsonDocument::Compact)); }
static QJsonObject obj(const QVariant &v) { return QJsonDocument::fromJson(v.toString().toUtf8()).object(); }

static QSqlQuery run(const QSqlDatabase &db, const QString &sql, const QVariantList &args = {})
{
    QSqlQuery q(db);
    q.prepare(sql);
    for (const auto &a : args)
        q.addBindValue(a);
    if (!q.exec())
        qWarning("luma store: %s: %s", qPrintable(sql.left(60)), qPrintable(q.lastError().text()));
    return q;
}

Store::Store(const QString &path)
    : m_connection(QStringLiteral("luma-") + QUuid::createUuid().toString(QUuid::WithoutBraces))
{
    auto d = QSqlDatabase::addDatabase(QStringLiteral("QSQLITE"), m_connection);
    d.setDatabaseName(path);
    if (!d.open())
        qFatal("luma store: cannot open %s: %s", qPrintable(path), qPrintable(d.lastError().text()));
    for (const char *sql : {
             "PRAGMA journal_mode=WAL",
             "CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, last_activity TEXT NOT NULL, json TEXT NOT NULL)",
             "CREATE TABLE IF NOT EXISTS messages (conversation_id TEXT NOT NULL, seq INTEGER NOT NULL, json TEXT NOT NULL,"
             " PRIMARY KEY (conversation_id, seq))",
             "CREATE TABLE IF NOT EXISTS outbox (client_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, body TEXT NOT NULL,"
             " created_at TEXT NOT NULL, state INTEGER NOT NULL)",
             "CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
         })
        run(d, QString::fromLatin1(sql));
}

Store::~Store()
{
    QSqlDatabase::database(m_connection).close();
    QSqlDatabase::removeDatabase(m_connection);
}

QSqlDatabase Store::db() const { return QSqlDatabase::database(m_connection, false); }

void Store::upsertConversation(const Conversation &c)
{
    run(db(), QStringLiteral("INSERT INTO conversations (id, last_activity, json) VALUES (?, ?, ?) "
                             "ON CONFLICT(id) DO UPDATE SET last_activity = excluded.last_activity, json = excluded.json"),
        {c.id, c.lastActivityAt.toUTC().toString(Qt::ISODateWithMs), json(c.toJson())});
}

std::optional<Conversation> Store::conversation(const QString &id) const
{
    auto q = run(db(), QStringLiteral("SELECT json FROM conversations WHERE id = ?"), {id});
    if (!q.next())
        return std::nullopt;
    return Conversation::fromJson(obj(q.value(0)));
}

QList<Conversation> Store::conversations() const
{
    QList<Conversation> out;
    auto q = run(db(), QStringLiteral("SELECT json FROM conversations ORDER BY last_activity DESC, id"));
    while (q.next())
        out.append(Conversation::fromJson(obj(q.value(0))));
    return out;
}

void Store::removeConversation(const QString &id)
{
    run(db(), QStringLiteral("DELETE FROM messages WHERE conversation_id = ?"), {id});
    run(db(), QStringLiteral("DELETE FROM conversations WHERE id = ?"), {id});
}

void Store::upsertMessage(const Message &m)
{
    run(db(), QStringLiteral("INSERT INTO messages (conversation_id, seq, json) VALUES (?, ?, ?) "
                             "ON CONFLICT(conversation_id, seq) DO UPDATE SET json = excluded.json"),
        {m.conversationId, m.seq, json(m.toJson())});
}

QList<Message> Store::messages(const QString &conversationId, int limit) const
{
    QList<Message> out;
    auto q = run(db(), QStringLiteral("SELECT json FROM (SELECT seq, json FROM messages WHERE conversation_id = ? "
                                      "ORDER BY seq DESC LIMIT ?) ORDER BY seq ASC"),
                 {conversationId, limit});
    while (q.next())
        out.append(Message::fromJson(obj(q.value(0))));
    return out;
}

void Store::addOutbox(const OutboxItem &i)
{
    run(db(), QStringLiteral("INSERT OR REPLACE INTO outbox (client_id, conversation_id, body, created_at, state) VALUES (?, ?, ?, ?, ?)"),
        {i.clientId.toString(QUuid::WithoutBraces), i.conversationId, i.body, i.createdAt.toUTC().toString(Qt::ISODateWithMs), int(i.state)});
}

void Store::removeOutbox(const QUuid &clientId)
{
    run(db(), QStringLiteral("DELETE FROM outbox WHERE client_id = ?"), {clientId.toString(QUuid::WithoutBraces)});
}

void Store::setOutboxState(const QUuid &clientId, OutboxItem::State state)
{
    run(db(), QStringLiteral("UPDATE outbox SET state = ? WHERE client_id = ?"), {int(state), clientId.toString(QUuid::WithoutBraces)});
}

QList<OutboxItem> Store::outbox() const
{
    QList<OutboxItem> out;
    auto q = run(db(), QStringLiteral("SELECT client_id, conversation_id, body, created_at, state FROM outbox ORDER BY created_at"));
    while (q.next())
        out.append({QUuid::fromString(q.value(0).toString()), q.value(1).toString(), q.value(2).toString(),
                    QDateTime::fromString(q.value(3).toString(), Qt::ISODateWithMs), OutboxItem::State(q.value(4).toInt())});
    return out;
}

std::optional<qint64> Store::cursor() const
{
    auto q = run(db(), QStringLiteral("SELECT value FROM kv WHERE key = 'cursor'"));
    if (!q.next())
        return std::nullopt;
    return q.value(0).toLongLong();
}

void Store::setCursor(qint64 cursor)
{
    run(db(), QStringLiteral("INSERT INTO kv (key, value) VALUES ('cursor', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"),
        {QString::number(cursor)});
}

void Store::clear()
{
    for (const char *t : {"conversations", "messages", "outbox", "kv"})
        run(db(), QStringLiteral("DELETE FROM ") + QLatin1StringView(t));
}
