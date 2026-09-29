#pragma once

#include <QString>

namespace Markdown {
// Replaces images (![alt](url) and <img ...>) with their alt text. The README
// is displayed with Text.MarkdownText, which would download remote images on
// its own; the screenshots are already shown separately, through the daemon.
QString stripImages(const QString &markdown);
} // namespace Markdown
