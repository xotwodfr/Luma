#pragma once

#include <QQuickImageProvider>

// Renders Lucide-style SVG icons in any color: image://icon/<name>?color=%23RRGGBB.
// Works on every scene-graph backend (including software), unlike shader-based tinting.
class IconProvider : public QQuickImageProvider {
public:
    explicit IconProvider(QString dir);
    QImage requestImage(const QString &id, QSize *size, const QSize &requestedSize) override;

private:
    QString m_dir;
};
