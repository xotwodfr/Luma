#include "Format.h"

#include <QLocale>
#include <QStringList>

namespace fmt {

QString timeOfDay(const QDateTime &t) { return t.toLocalTime().toString(u"HH:mm"_s); }

QString listTime(const QDateTime &t, const QDateTime &now)
{
    const QDate d = t.toLocalTime().date(), today = now.toLocalTime().date();
    const qint64 days = d.daysTo(today);
    if (days == 0)
        return timeOfDay(t);
    if (days == 1)
        return u"Yesterday"_s;
    if (days > 1 && days < 7)
        return QLocale().dayName(d.dayOfWeek(), QLocale::ShortFormat);
    if (d.year() == today.year())
        return QLocale().toString(d, u"d MMM"_s);
    return QLocale().toString(d, u"d MMM yyyy"_s);
}

QString dayLabel(const QDate &d, const QDate &today)
{
    const qint64 days = d.daysTo(today);
    if (days == 0)
        return u"Today"_s;
    if (days == 1)
        return u"Yesterday"_s;
    if (days > 1 && days < 7)
        return QLocale().dayName(d.dayOfWeek());
    return QLocale().toString(d, d.year() == today.year() ? u"d MMMM"_s : u"d MMMM yyyy"_s);
}

QString initials(const QString &name)
{
    const QStringList words = name.simplified().split(u' ', Qt::SkipEmptyParts);
    if (words.isEmpty())
        return u"?"_s;
    QString out = words.first().left(1).toUpper();
    if (words.size() > 1)
        out += words.last().left(1).toUpper();
    return out;
}

} // namespace fmt
