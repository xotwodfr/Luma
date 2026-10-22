#include <QtTest>
#include <QGuiApplication>

#include "IconProvider.h"

class TestIcons : public QObject {
    Q_OBJECT
private slots:
    void rendersColored()
    {
        IconProvider p(QStringLiteral(LUMA_ICON_DIR));
        QSize size;
        const QImage img = p.requestImage(QStringLiteral("search?color=%230E1418"), &size, QSize(40, 40));
        QVERIFY(!img.isNull());
        QCOMPARE(img.size(), QSize(40, 40));
        bool found = false;
        for (int y = 0; y < img.height() && !found; ++y)
            for (int x = 0; x < img.width() && !found; ++x) {
                const QColor c = img.pixelColor(x, y);
                found = c.alpha() == 255 && c.red() == 0x0E && c.green() == 0x14 && c.blue() == 0x18;
            }
        QVERIFY2(found, "expected an opaque pixel in the requested color");
    }

    void unknownIsNull()
    {
        IconProvider p(QStringLiteral(LUMA_ICON_DIR));
        QSize size;
        QVERIFY(p.requestImage(QStringLiteral("no-such-icon"), &size, QSize(24, 24)).isNull());
        QVERIFY(p.requestImage(QStringLiteral("../../etc/passwd"), &size, QSize(24, 24)).isNull());
    }
};

QTEST_MAIN(TestIcons)
#include "tst_icons.moc"
