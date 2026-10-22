#pragma once

#include <QDateTime>
#include <QString>

using namespace Qt::StringLiterals;

// Display formatting shared by the models (DESIGN.md §7).
namespace fmt {
// "14:08" today, "Yesterday", weekday within the last week, "2 Oct", or "30 Dec 2025".
QString listTime(const QDateTime &t, const QDateTime &now);
// Date separator above a message group: "Today", "Yesterday", "Tuesday", "2 October", "30 December 2025".
QString dayLabel(const QDate &d, const QDate &today);
QString timeOfDay(const QDateTime &t);
// Up to two initials: first and last word.
QString initials(const QString &name);
}
