#include "IconProvider.h"

#include <QFile>
#include <QPainter>
#include <QRegularExpression>
#include <QSvgRenderer>
#include <QUrlQuery>

IconProvider::IconProvider(QString dir)
    : QQuickImageProvider(QQuickImageProvider::Image)
    , m_dir(std::move(dir))
{
}

QImage IconProvider::requestImage(const QString &id, QSize *size, const QSize &requestedSize)
{
    const QString name = id.section(u'?', 0, 0);
    static const QRegularExpression valid(QStringLiteral("^[a-z0-9-]+$"));
    if (!valid.match(name).hasMatch())
        return {};
    QFile f(m_dir + u'/' + name + u".svg");
    if (!f.open(QIODevice::ReadOnly))
        return {};

    const QColor color(QUrlQuery(id.section(u'?', 1)).queryItemValue(QStringLiteral("color"), QUrl::FullyDecoded));
    QByteArray svg = f.readAll();
    svg.replace("currentColor", (color.isValid() ? color : QColor(Qt::black)).name().toLatin1());

    const QSize out = requestedSize.isValid() && !requestedSize.isEmpty() ? requestedSize : QSize(24, 24);
    QImage img(out, QImage::Format_ARGB32_Premultiplied);
    img.fill(Qt::transparent);
    QSvgRenderer renderer(svg);
    QPainter p(&img);
    p.setRenderHint(QPainter::Antialiasing);
    renderer.render(&p);
    if (size)
        *size = out;
    return img;
}
