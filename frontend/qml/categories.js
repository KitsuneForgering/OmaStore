.pragma library

// How a category is written when the user reads it. Everywhere else the store
// keeps the freedesktop name (AudioVideo): the manifest, the .desktop entry,
// the CLI and the JSON. Only the text on screen changes, and only here.

function display(name) {
    return name === "AudioVideo" ? "Audio/Video" : (name || "")
}
