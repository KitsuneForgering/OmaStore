#include "markdown.h"

#include <QRegularExpression>

namespace {
QString protectCodeSpans(const QString &text, QStringList &spans);

QString stripImagesFromProse(const QString &markdown)
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

// Code spans hold literal text (`<br>` documents a tag): they are swapped for
// private-use placeholders while the HTML is simplified, then restored. A span
// closes on a backtick run of the same length and never crosses a blank line.
QString protectCodeSpans(const QString &text, QStringList &spans)
{
    QString out;
    qsizetype i = 0;
    while (i < text.size()) {
        if (text[i] != QLatin1Char('`')) {
            out += text[i++];
            continue;
        }
        qsizetype run = i;
        while (run < text.size() && text[run] == QLatin1Char('`'))
            ++run;
        const qsizetype n = run - i;
        qsizetype close = -1;
        for (qsizetype j = run; j < text.size();) {
            if (text[j] == QLatin1Char('\n')) {
                const qsizetype next = text.indexOf(QLatin1Char('\n'), j + 1);
                if (text.mid(j + 1, next < 0 ? -1 : next - j - 1).trimmed().isEmpty())
                    break; // blank line (or end of text): a paragraph ends here
            }
            if (text[j] != QLatin1Char('`')) {
                ++j;
                continue;
            }
            qsizetype end = j;
            while (end < text.size() && text[end] == QLatin1Char('`'))
                ++end;
            if (end - j == n) {
                close = end;
                break;
            }
            j = end;
        }
        if (close < 0) {
            out += text.mid(i, n);
            i = run;
            continue;
        }
        out += QChar(0xE000) + QString::number(spans.size()) + QChar(0xE001);
        spans += text.mid(i, close - i);
        i = close;
    }
    return out;
}

QString simplifyHtml(const QString &source)
{
    QStringList spans;
    QString text = protectCodeSpans(source, spans);
    static const auto insensitive = QRegularExpression::CaseInsensitiveOption;
    static const QRegularExpression anchor(
        QStringLiteral(R"html(<a\b[^>]*\bhref\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a\s*>)html"),
        insensitive | QRegularExpression::DotMatchesEverythingOption);
    static const QRegularExpression lineBreak(QStringLiteral(R"(<br\b[^>]*>)"), insensitive);
    static const QRegularExpression paragraph(QStringLiteral(R"(</?p\b[^>]*>)"), insensitive);
    static const QRegularExpression block(
        QStringLiteral(R"(</?(?:div|center|details|summary|table|tr|blockquote)\b[^>]*>)"), insensitive);
    static const QRegularExpression listItem(QStringLiteral(R"(<li\b[^>]*>)"), insensitive);
    static const QRegularExpression endListItem(QStringLiteral(R"(</li\s*>)"), insensitive);
    static const QRegularExpression inlineTag(
        QStringLiteral(R"(</?(?:a|strong|em|b|i|u|sub|sup|small|span|code|kbd|ul|ol|td|th|pre)\b[^>]*>)"),
        insensitive);

    text.replace(anchor, QStringLiteral("[\\2](\\1)"));
    text.replace(lineBreak, QStringLiteral("\n"));
    text.replace(paragraph, QStringLiteral("\n\n"));
    for (int level = 1; level <= 6; ++level) {
        const QRegularExpression open(QStringLiteral("<h%1\\b[^>]*>").arg(level), insensitive);
        const QRegularExpression close(QStringLiteral("</h%1\\s*>").arg(level), insensitive);
        text.replace(open, QString(level, QLatin1Char('#')) + QLatin1Char(' '));
        text.replace(close, QStringLiteral("\n\n"));
    }
    text.replace(block, QStringLiteral("\n\n"));
    text.replace(listItem, QStringLiteral("\n- "));
    text.replace(endListItem, QStringLiteral("\n"));
    text.replace(inlineTag, QString());
    static const QRegularExpression placeholder(QStringLiteral("\uE000(\\d+)\uE001"));
    QString result;
    qsizetype last = 0;
    for (auto it = placeholder.globalMatch(text); it.hasNext();) {
        const auto m = it.next();
        result += text.mid(last, m.capturedStart() - last) + spans.value(m.captured(1).toInt());
        last = m.capturedEnd();
    }
    return result + text.mid(last);
}
} // namespace

QString Markdown::stripImages(const QString &markdown)
{
    QStringList spans;
    const QString protectedText = protectCodeSpans(markdown, spans);
    QString result = stripImagesFromProse(protectedText);
    static const QRegularExpression placeholder(QStringLiteral("\uE000(\\d+)\uE001"));
    QString restored;
    qsizetype last = 0;
    for (auto it = placeholder.globalMatch(result); it.hasNext();) {
        const auto m = it.next();
        restored += result.mid(last, m.capturedStart() - last) + spans.value(m.captured(1).toInt());
        last = m.capturedEnd();
    }
    return restored + result.mid(last);
}

QString Markdown::forDisplay(const QString &markdown)
{
    const QString &input = markdown;
    static const QRegularExpression fence(QStringLiteral(R"(^ {0,3}(`{3,}|~{3,}))"));
    QString output, prose;
    QChar fenceChar;
    int fenceLength = 0;
    // Indented code starts after a blank line. An indented paragraph inside a
    // list looks the same and is also kept verbatim, which only leaves its HTML
    // unsimplified.
    bool afterBlank = true, indented = false;
    for (const QString &line : input.split(QLatin1Char('\n'), Qt::KeepEmptyParts)) {
        const auto match = fence.match(line);
        const bool blank = line.trimmed().isEmpty();
        if (fenceLength == 0 && match.hasMatch()) {
            output += simplifyHtml(stripImages(prose));
            prose.clear();
            fenceChar = match.captured(1).at(0);
            fenceLength = match.captured(1).size();
            output += line + QLatin1Char('\n');
            indented = false;
        } else if (fenceLength > 0) {
            output += line + QLatin1Char('\n');
            if (match.hasMatch() && match.captured(1).at(0) == fenceChar &&
                match.captured(1).size() >= fenceLength)
                fenceLength = 0;
        } else if (!blank && (afterBlank || indented) &&
                   (line.startsWith(QLatin1String("    ")) || line.startsWith(QLatin1Char('\t')))) {
            output += simplifyHtml(stripImages(prose));
            prose.clear();
            output += line + QLatin1Char('\n');
            indented = true;
        } else {
            prose += line + QLatin1Char('\n');
            if (!blank)
                indented = false;
        }
        afterBlank = blank || fenceLength > 0 || match.hasMatch();
    }
    return output + simplifyHtml(stripImages(prose));
}
