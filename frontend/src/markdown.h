#pragma once

#include <QString>
#include <QStringList>

namespace Markdown {
// Replaces images (![alt](url) and <img ...>) with their alt text. The README
// is displayed with Text.MarkdownText, which would download remote images on
// its own; the screenshots are already shown separately, through the daemon.
QString stripImages(const QString &markdown);
// Removes HTML presentation tags that Qt's Markdown parser cannot handle
// reliably, while preserving ordinary Markdown and fenced code blocks.
QString forDisplay(const QString &markdown);
// Splits display Markdown into consecutive sections, each rendered by its own
// Text item: Qt 6.12's scene graph crashes rendering one Text holding a long
// document with a table (issue #15). Sections start at headings; a section
// longer than maxChars is cut further at blank lines between blocks. Code
// fences are never cut, and link reference definitions are repeated in every
// section so reference-style links keep working.
QStringList sections(const QString &markdown, int maxChars = 4000);
} // namespace Markdown
