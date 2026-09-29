#pragma once

#include <QString>

namespace Markdown {
// Troca imagens (![alt](url) e <img ...>) pelo texto alternativo. O README
// é exibido com Text.MarkdownText, que baixaria as imagens remotas por conta
// própria; as screenshots já são mostradas à parte, via daemon.
QString stripImages(const QString &markdown);
} // namespace Markdown
