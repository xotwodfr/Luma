#include <QtTest>
#include <random>

#include "Backoff.h"

using namespace std::chrono;

class TestBackoff : public QObject {
    Q_OBJECT
private slots:
    void fullJitterWithinBounds()
    {
        std::mt19937 gen(7);
        std::uniform_real_distribution<double> u(0.0, 1.0);
        Backoff b([&] { return u(gen); });
        const qint64 caps[] = {500, 1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000};
        for (qint64 cap : caps) {
            const auto d = b.next();
            QVERIFY2(d.count() >= 0 && d.count() <= cap, qPrintable(QString::number(d.count())));
        }
        for (int i = 0; i < 1000; ++i)
            QVERIFY(b.next() <= 30000ms);
    }

    void maxRandomHitsCap()
    {
        Backoff b([] { return 0.999999; });
        for (int i = 0; i < 20; ++i)
            b.next();
        QVERIFY(b.next() >= 29999ms);
    }

    void resetStartsOver()
    {
        Backoff b([] { return 0.999999; });
        for (int i = 0; i < 6; ++i)
            b.next();
        b.reset();
        QVERIFY(b.next() <= 500ms);
    }
};

QTEST_GUILESS_MAIN(TestBackoff)
#include "tst_backoff.moc"
