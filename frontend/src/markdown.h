#pragma once

#include <QString>

namespace Markdown {
// Replaces images (![alt](url) and <img ...>) with their alt text. The README
// is displayed with Text.MarkdownText, which would download remote images on
// its own; the screenshots are already shown separately, through the daemon.
QString stripImages(const QString &markdown);
// Removes HTML presentation tags that Qt's Markdown parser cannot handle
// reliably, while preserving ordinary Markdown and fenced code blocks.
QString forDisplay(const QString &markdown);
} // namespace Markdown
