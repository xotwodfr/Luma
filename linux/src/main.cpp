#include <QCommandLineParser>
#include <QFontDatabase>
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>
#include <QQuickWindow>
#include <QTimer>

#include "IconProvider.h"

// The platform theme normally supplies the desktop's UI font (Noto Sans on KDE, Cantarell or
// Adwaita Sans on GNOME). Headless platforms fall back to a generic face; pick a real one.
static void ensureUiFont()
{
    const QString platform = QGuiApplication::platformName();
    if (platform != u"offscreen" && platform != u"minimal")
        return;
    const QStringList families = QFontDatabase::families();
    for (const char *candidate : {"Noto Sans", "Adwaita Sans", "Cantarell"}) {
        if (families.contains(QLatin1StringView(candidate))) {
            QFont f(QString::fromLatin1(candidate));
            f.setPointSizeF(10.5);
            QGuiApplication::setFont(f);
            return;
        }
    }
}

int main(int argc, char *argv[])
{
    QGuiApplication app(argc, argv);
    QGuiApplication::setApplicationName(QStringLiteral("Luma"));
    QGuiApplication::setOrganizationName(QStringLiteral("Luma"));
    QGuiApplication::setApplicationVersion(QStringLiteral(PROJECT_VERSION_STRING));
    QGuiApplication::setDesktopFileName(QStringLiteral("app.luma.Luma"));

    QCommandLineParser cli;
    cli.addHelpOption();
    cli.addVersionOption();
    QCommandLineOption server(QStringLiteral("server"), QStringLiteral("Server URL."), QStringLiteral("url"),
                              qEnvironmentVariable("LUMA_SERVER", QStringLiteral("https://chat.folk3s.com")));
    QCommandLineOption profile(QStringLiteral("profile"), QStringLiteral("Data directory."), QStringLiteral("dir"));
    QCommandLineOption theme(QStringLiteral("theme"), QStringLiteral("light, dark or system."), QStringLiteral("theme"), QStringLiteral("system"));
    QCommandLineOption shot(QStringLiteral("screenshot"), QStringLiteral("Save a screenshot and exit."), QStringLiteral("file"));
    QCommandLineOption size(QStringLiteral("size"), QStringLiteral("Window size WxH."), QStringLiteral("size"), QStringLiteral("1280x800"));
    QCommandLineOption after(QStringLiteral("after-ms"), QStringLiteral("Screenshot delay."), QStringLiteral("ms"), QStringLiteral("1500"));
    cli.addOptions({server, profile, theme, shot, size, after});
    cli.process(app);

    ensureUiFont();
    QFontDatabase::addApplicationFont(QStringLiteral(":/qt/qml/Luma/assets/fonts/InstrumentSans.ttf"));
    QQuickStyle::setStyle(QStringLiteral("Basic"));

    QQmlApplicationEngine engine;
    engine.addImageProvider(QStringLiteral("icon"), new IconProvider(QStringLiteral(":/qt/qml/Luma/assets/icons")));
    const QStringList wh = cli.value(size).split(u'x');
    engine.setInitialProperties({
        {QStringLiteral("forcedTheme"), cli.value(theme)},
        {QStringLiteral("width"), wh.value(0).toInt()},
        {QStringLiteral("height"), wh.value(1).toInt()},
    });
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app, [] { QCoreApplication::exit(1); },
                     Qt::QueuedConnection);
    engine.loadFromModule("Luma", "Main");

    if (cli.isSet(shot)) {
        auto *window = qobject_cast<QQuickWindow *>(engine.rootObjects().value(0));
        const QString file = cli.value(shot);
        QTimer::singleShot(cli.value(after).toInt(), &app, [window, file] {
            const bool ok = window && window->grabWindow().save(file);
            QCoreApplication::exit(ok ? 0 : 2);
        });
    }
    return app.exec();
}
