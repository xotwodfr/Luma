#include "AppController.h"

#include <QDir>
#include <QFile>
#include <QGuiApplication>
#include <QJsonDocument>
#include <qt6keychain/keychain.h>

#include "Format.h"

using namespace Qt::StringLiterals;

static const QString kService = u"Luma"_s;

// Development and screenshot runs without a keyring may opt into a plain file (never the default).
static QString insecureTokenFile() { return qEnvironmentVariable("LUMA_INSECURE_TOKEN_FILE"); }

AppController::AppController(const QUrl &server, const QString &profileDir, QObject *parent)
    : QObject(parent)
{
    QDir().mkpath(profileDir);
    m_store = std::make_unique<Store>(QDir(profileDir).filePath(server.host() + u".db"_s));
    m_api = std::make_unique<ApiClient>(server);
    m_transport = std::make_unique<RealtimeClient>(m_api->realtimeUrl());
    m_sync = std::make_unique<Sync>(*m_store, *m_api, *m_transport);
    m_conversations = std::make_unique<ConversationListModel>(*m_store, *m_sync);
    m_messages = std::make_unique<MessageListModel>(*m_store, *m_sync);

    connect(m_transport.get(), &Transport::stateChanged, this, &AppController::connectionChanged);
    connect(m_sync.get(), &Sync::readyChanged, this, &AppController::connectionChanged);
    connect(m_sync.get(), &Sync::signedOut, this, [this](const QString &reason) {
        endSession(reason == u"revoked" ? u"This device was signed out from another session."_s : u"You were signed out."_s);
    });
    connect(m_sync.get(), &Sync::conversationsChanged, this, [this] {
        if (!m_synced) {
            m_synced = true;
            emit syncedChanged();
        }
        emit headerChanged();
        markVisibleRead();
    });
    connect(m_sync.get(), &Sync::typingChanged, this, [this](const QString &conv) {
        if (conv == m_current)
            emit headerChanged();
    });
    connect(m_sync.get(), &Sync::presenceChanged, this, &AppController::headerChanged);
    connect(m_sync.get(), &Sync::messagesChanged, this, [this](const QString &conv) {
        if (conv == m_current)
            markVisibleRead();
    });
    // While reconnecting, "Reconnecting in N s" counts down.
    m_tick.setInterval(1000);
    connect(&m_tick, &QTimer::timeout, this, &AppController::connectionChanged);
    m_tick.start();
    connect(qGuiApp, &QGuiApplication::applicationStateChanged, this, [this](Qt::ApplicationState s) {
        if (s == Qt::ApplicationActive)
            markVisibleRead();
    });
}

AppController::~AppController() { m_sync->stop(); }

QString AppController::connection() const
{
    switch (m_transport->state()) {
    case Transport::State::Online:
        return m_sync->isReady() ? u"online"_s : u"connecting"_s;
    case Transport::State::Reconnecting:
        return u"reconnecting"_s;
    case Transport::State::Offline:
        return u"offline"_s;
    default:
        return u"connecting"_s;
    }
}

QString AppController::sessionKey() const { return m_api->base().host(); }

void AppController::setPhase(const QString &p)
{
    if (p != m_phase) {
        m_phase = p;
        emit phaseChanged();
    }
}

void AppController::setError(const QString &e)
{
    if (e != m_error) {
        m_error = e;
        emit errorTextChanged();
    }
}

void AppController::restoreSession()
{
    auto use = [this](const QByteArray &blob) {
        const QJsonObject o = QJsonDocument::fromJson(blob).object();
        if (o[u"token"].toString().isEmpty())
            return setPhase(u"signedOut"_s);
        startSession(o[u"token"].toString(), o[u"me"].toObject().toVariantMap());
    };
    if (const QString file = insecureTokenFile(); !file.isEmpty()) {
        QFile f(file);
        use(f.open(QIODevice::ReadOnly) ? f.readAll() : QByteArray());
        return;
    }
    auto *job = new QKeychain::ReadPasswordJob(kService, this);
    job->setKey(sessionKey());
    connect(job, &QKeychain::Job::finished, this, [this, job, use] {
        use(job->error() == QKeychain::NoError ? job->binaryData() : QByteArray());
    });
    job->start();
}

void AppController::saveSession(const QString &token)
{
    const QByteArray blob = QJsonDocument(QJsonObject{{u"token"_s, token}, {u"me"_s, QJsonObject::fromVariantMap(m_me)}}).toJson(QJsonDocument::Compact);
    if (const QString file = insecureTokenFile(); !file.isEmpty()) {
        qWarning("luma: storing the session token in plain file %s (LUMA_INSECURE_TOKEN_FILE)", qPrintable(file));
        QFile f(file);
        if (f.open(QIODevice::WriteOnly)) {
            f.setPermissions(QFileDevice::ReadOwner | QFileDevice::WriteOwner);
            f.write(blob);
        }
        return;
    }
    auto *job = new QKeychain::WritePasswordJob(kService, this);
    job->setKey(sessionKey());
    job->setBinaryData(blob);
    connect(job, &QKeychain::Job::finished, this, [job] {
        if (job->error() != QKeychain::NoError)
            qWarning("luma: no system keyring (%s); you will need to sign in again next time", qPrintable(job->errorString()));
    });
    job->start();
}

void AppController::forgetSession()
{
    if (const QString file = insecureTokenFile(); !file.isEmpty()) {
        QFile::remove(file);
        return;
    }
    auto *job = new QKeychain::DeletePasswordJob(kService, this);
    job->setKey(sessionKey());
    job->start();
}

void AppController::begin(const QJsonObject &auth)
{
    const QJsonObject u = auth[u"user"].toObject();
    m_me = {{u"id"_s, u[u"id"].toString()}, {u"username"_s, u[u"username"].toString()},
            {u"displayName"_s, u[u"display_name"].toString()}, {u"initials"_s, fmt::initials(u[u"display_name"].toString())}};
    const QString token = auth[u"token"].toString();
    m_store->clear(); // a different account must never see the previous one's cache
    saveSession(token);
    startSession(token, m_me);
}

void AppController::startSession(const QString &token, const QVariantMap &me)
{
    m_me = me;
    emit meChanged();
    m_api->setToken(token);
    m_notice.clear();
    emit noticeChanged();
    m_synced = !m_store->conversations().isEmpty(); // cached list renders immediately
    emit syncedChanged();
    m_sync->start(token, m_me.value(u"id"_s).toString());
    m_conversations->refresh();
    if (!m_store->cursor())
        m_synced = false;
    setPhase(u"ready"_s);
}

void AppController::endSession(const QString &notice)
{
    m_sync->stop();
    m_api->setToken({});
    m_store->clear();
    forgetSession();
    m_current.clear();
    m_messages->setConversation({});
    m_conversations->setSelected({});
    m_conversations->refresh();
    m_me.clear();
    emit meChanged();
    emit currentChanged();
    m_notice = notice;
    emit noticeChanged();
    setPhase(u"signedOut"_s);
}

void AppController::signIn(const QString &username, const QString &password)
{
    if (username.trimmed().isEmpty() || password.isEmpty())
        return setError(u"Enter your username and password."_s);
    setError({});
    setPhase(u"signingIn"_s);
    m_api->login(username.trimmed(), password, [this](const ApiResult &r) {
        if (!r.ok()) {
            setPhase(u"signedOut"_s);
            return setError(r.errorMessage);
        }
        begin(r.body.object());
    });
}

void AppController::signUp(const QString &invite, const QString &username, const QString &displayName, const QString &password)
{
    setError({});
    setPhase(u"signingIn"_s);
    m_api->registerAccount(invite.trimmed(), username, displayName.trimmed(), password, [this](const ApiResult &r) {
        if (!r.ok()) {
            setPhase(u"signedOut"_s);
            return setError(r.errorMessage);
        }
        begin(r.body.object());
    });
}

void AppController::signOut()
{
    m_api->logout([](const ApiResult &) {}); // best effort; local state goes regardless
    endSession({});
}

void AppController::openConversation(const QString &id)
{
    if (id == m_current)
        return;
    m_current = id;
    m_conversations->setSelected(id);
    m_messages->setConversation(id);
    emit currentChanged();
    emit headerChanged();
    markVisibleRead();
}

void AppController::startDirect(const QString &username)
{
    const QString name = username.trimmed().remove(u'@');
    if (name.isEmpty())
        return emit startDirectFinished(false, u"Enter a username."_s);
    m_api->openDirect(name, [this](const ApiResult &r) {
        if (!r.ok()) {
            const QString msg = r.errorCode == u"not_found" ? u"No one with that username."_s : r.errorMessage;
            return emit startDirectFinished(false, msg);
        }
        const auto c = Conversation::fromJson(r.body.object());
        m_store->upsertConversation(c);
        m_conversations->refresh();
        openConversation(c.id);
        emit startDirectFinished(true, {});
    });
}

void AppController::send(const QString &text)
{
    if (!m_current.isEmpty())
        m_sync->send(m_current, text);
}

void AppController::retry(const QString &clientId) { m_sync->retry(QUuid::fromString(clientId)); }

void AppController::markVisibleRead()
{
    if (!m_current.isEmpty() && qGuiApp->applicationState() == Qt::ApplicationActive)
        m_sync->markRead(m_current);
}

void AppController::composerEdited()
{
    if (!m_current.isEmpty())
        m_sync->typing(m_current);
}

QVariantMap AppController::header() const
{
    const auto c = m_store->conversation(m_current);
    if (!c)
        return {};
    const QString me = m_me.value(u"id"_s).toString();
    const QString title = ConversationListModel::titleFor(*c, me);
    bool online = false;
    QStringList pair;
    for (const auto &m : c->members) {
        if (m.userId == me)
            continue;
        online = online || (c->kind == u"direct" && m_sync->isOnline(m.userId));
        if (pair.size() < 2)
            pair << fmt::initials(m.displayName).left(1);
    }
    QString status = c->kind == u"group" ? u"%1 members"_s.arg(c->members.size()) : online ? u"Online"_s : QString();
    const QStringList typing = m_sync->typingUsers(m_current);
    if (!typing.isEmpty())
        status = u"typing…"_s;
    return {{u"title"_s, title}, {u"initials"_s, fmt::initials(title)}, {u"pair"_s, pair}, {u"kind"_s, c->kind},
            {u"online"_s, online}, {u"status"_s, status}, {u"typing"_s, !typing.isEmpty()},
            {u"firstName"_s, title.section(u' ', 0, 0)}};
}
