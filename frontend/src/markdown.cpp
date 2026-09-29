#include "markdown.h"

#include <QRegularExpression>

QString Markdown::stripImages(const QString &markdown)
{
    static const QRegularExpression mdImage(QStringLiteral(R"(!\[([^\]]*)\]\([^)]*\))"));
    static const QRegularExpression refImage(QStringLiteral(R"(!\[([^\]]*)\]\[[^\]]*\])"));
    static const QRegularExpression htmlImg(QStringLiteral(R"(<img\b[^>]*>)"),
                                            QRegularExpression::CaseInsensitiveOption);
    static const QRegularExpression altAttr(QStringLiteral(R"(\balt\s*=\s*["']([^"']*)["'])"),
                                            QRegularExpression::CaseInsensitiveOption);
    static const QRegularExpression htmlPicture(QStringLiteral(R"(<(picture|source|video)\b[^>]*>|</(picture|video)>)"),
                                                QRegularExpression::CaseInsensitiveOption);

    QString out = markdown;
    out.replace(mdImage, QStringLiteral("\\1"));
    out.replace(refImage, QStringLiteral("\\1"));
    out.replace(htmlPicture, QString());

    QString result;
    qsizetype last = 0;
    auto it = htmlImg.globalMatch(out);
    while (it.hasNext()) {
        const auto m = it.next();
        result += out.mid(last, m.capturedStart() - last);
        const auto alt = altAttr.match(m.captured());
        if (alt.hasMatch())
            result += alt.captured(1);
        last = m.capturedEnd();
    }
    result += out.mid(last);
    return result;
}
