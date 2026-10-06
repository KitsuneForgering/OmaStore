.pragma library

// How a category is written when the user reads it. Everywhere else the store
// keeps the freedesktop name (AudioVideo): the manifest, the .desktop entry,
// the CLI and the JSON. Only the text on screen changes, and only here.

// The main freedesktop categories, marked for translation.
var names = {
    "AudioVideo": QT_TRANSLATE_NOOP("Categories", "Audio/Video"),
    "Development": QT_TRANSLATE_NOOP("Categories", "Development"),
    "Education": QT_TRANSLATE_NOOP("Categories", "Education"),
    "Game": QT_TRANSLATE_NOOP("Categories", "Game"),
    "Graphics": QT_TRANSLATE_NOOP("Categories", "Graphics"),
    "Network": QT_TRANSLATE_NOOP("Categories", "Network"),
    "Office": QT_TRANSLATE_NOOP("Categories", "Office"),
    "Science": QT_TRANSLATE_NOOP("Categories", "Science"),
    "Settings": QT_TRANSLATE_NOOP("Categories", "Settings"),
    "System": QT_TRANSLATE_NOOP("Categories", "System"),
    "Utility": QT_TRANSLATE_NOOP("Categories", "Utility")
}

function display(name) {
    return names[name] !== undefined ? qsTranslate("Categories", names[name]) : (name || "")
}
