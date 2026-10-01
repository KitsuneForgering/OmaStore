#include "theme.h"

#include <QDir>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

class TestTheme : public QObject {
    Q_OBJECT

    static void write(const QString &path, const QByteArray &data)
    {
        QDir().mkpath(QFileInfo(path).path());
        QFile f(path);
        QVERIFY(f.open(QIODevice::WriteOnly | QIODevice::Truncate));
        f.write(data);
    }

private slots:
    void parse()
    {
        const auto c = Theme::parseColors(
            "mode = \"light\"\n"
            "# comment\n"
            "accent = \"#3ee8ff\"   # cyan\n"
            "background='#08090a'\n"
            "[section]\n"
            "garbage without equals\n");
        QCOMPARE(c.value("mode"), QStringLiteral("light"));
        QCOMPARE(c.value("accent"), QStringLiteral("#3ee8ff"));
        QCOMPARE(c.value("background"), QStringLiteral("#08090a"));
        QCOMPARE(c.size(), 3);
    }

    void loadsFirstExistingDirAndFallbacks()
    {
        QTemporaryDir tmp;
        const QString state = tmp.path() + "/state/current/theme";
        const QString config = tmp.path() + "/config/current/theme";
        write(config + "/colors.toml", "accent = \"#ff0000\"\n");
        write(state + "/colors.toml", "mode = \"light\"\naccent = \"#3ee8ff\"\nforeground = \"invalid\"\n");
        write(tmp.path() + "/state/current/theme.name", "sword-art-omarchy\n");

        Theme t({state, config});
        // The fill keeps the theme's accent (black text reads on it); as text on
        // the light background it is darkened to 7:1.
        QCOMPARE(t.accentFill(), QColor("#3ee8ff"));
        QVERIFY(Theme::contrast(t.accent(), t.background()) >= Theme::TextContrast);
        QVERIFY(!t.dark());
        QCOMPARE(t.name(), QStringLiteral("sword-art-omarchy"));
        QVERIFY(t.foreground().isValid()); // an invalid color falls back to the default
        QVERIFY(t.background().isValid());
    }

    void contrastMath()
    {
        QCOMPARE(qRound(Theme::contrast(Qt::black, Qt::white) * 10), 210);
        QCOMPARE(Theme::contrast(QColor("#777777"), QColor("#777777")), 1.0);
        // A color that already passes is left alone.
        QCOMPARE(Theme::ensureContrast(Qt::white, {Qt::black}, 7), QColor(Qt::white));
        // Otherwise it moves toward white on dark backgrounds, toward black on light ones.
        const QColor onDark = Theme::ensureContrast(QColor("#666666"), {QColor("#222222")}, 7);
        QVERIFY(Theme::contrast(onDark, QColor("#222222")) >= 7);
        QVERIFY(onDark.lightness() > QColor("#666666").lightness());
        const QColor onLight = Theme::ensureContrast(QColor("#acb0be"), {QColor("#eff1f5")}, 7);
        QVERIFY(Theme::contrast(onLight, QColor("#eff1f5")) >= 7);
        QVERIFY(onLight.lightness() < QColor("#acb0be").lightness());
        // The hue survives: a blue stays blue.
        const QColor blue = Theme::ensureContrast(QColor("#1e66f5"), {QColor("#eff1f5")}, 7);
        QVERIFY(blue.blue() > blue.red() && blue.blue() > blue.green());
    }

    // Real Omarchy palettes whose own colors miss WCAG AAA (several text
    // pairs at 1.3:1 to 5:1): every derived pair must reach the targets.
    void accessibleOnOmarchyThemes_data()
    {
        QTest::addColumn<QByteArray>("toml");
        QTest::addColumn<bool>("dark");
        QTest::newRow("catppuccin-latte") << QByteArray(
            "mode = \"light\"\naccent = \"#1e66f5\"\nselection = \"#ccd0da\"\nmuted = \"#acb0be\"\n"
            "background = \"#eff1f5\"\nlighter_background = \"#dce0e8\"\nforeground = \"#4c4f69\"\n"
            "red = \"#d20f39\"\nyellow = \"#df8e1d\"\ngreen = \"#40a02b\"\n") << false;
        QTest::newRow("rose-pine") << QByteArray(
            "mode = \"light\"\naccent = \"#56949f\"\nselection = \"#dfdad9\"\nmuted = \"#cecacd\"\n"
            "background = \"#faf4ed\"\nlighter_background = \"#f2e9e1\"\nforeground = \"#575279\"\n"
            "red = \"#b4637a\"\nyellow = \"#ea9d34\"\ngreen = \"#286983\"\n") << false;
        QTest::newRow("white") << QByteArray(
            "mode = \"light\"\naccent = \"#6e6e6e\"\nselection = \"#c0c0c0\"\nmuted = \"#808080\"\n"
            "background = \"#ffffff\"\nlighter_background = \"#c0c0c0\"\nforeground = \"#000000\"\n"
            "red = \"#2a2a2a\"\nyellow = \"#4a4a4a\"\ngreen = \"#3a3a3a\"\n") << false;
        QTest::newRow("miasma") << QByteArray(
            "mode = \"dark\"\naccent = \"#78824b\"\nselection = \"#383838\"\nmuted = \"#666666\"\n"
            "background = \"#222222\"\nlighter_background = \"#2c2c2c\"\nforeground = \"#c2c2b0\"\n"
            "red = \"#685742\"\nyellow = \"#b36d43\"\ngreen = \"#5f875f\"\n") << true;
        QTest::newRow("everforest") << QByteArray(
            "mode = \"dark\"\naccent = \"#7fbbb3\"\nselection = \"#3d484d\"\nmuted = \"#475258\"\n"
            "background = \"#2d353b\"\nlighter_background = \"#343f44\"\nforeground = \"#d3c6aa\"\n"
            "red = \"#e67e80\"\nyellow = \"#dbbc7f\"\ngreen = \"#a7c080\"\n") << true;
        // No mode key: light or dark is judged by the background.
        QTest::newRow("no mode, light") << QByteArray("background = \"#fafafa\"\nforeground = \"#999999\"\n") << false;
        QTest::newRow("no mode, dark") << QByteArray("background = \"#101010\"\nmuted = \"#303030\"\n") << true;
    }

    void accessibleOnOmarchyThemes()
    {
        QFETCH(QByteArray, toml);
        QFETCH(bool, dark);
        QTemporaryDir tmp;
        write(tmp.path() + "/colors.toml", toml);
        Theme t({tmp.path()});
        QCOMPARE(t.dark(), dark);

        const QList<QColor> grounds{t.background(), t.surface(), t.selection(), t.hover()};
        const QList<std::pair<const char *, QColor>> text{
            {"foreground", t.foreground()}, {"muted", t.muted()}, {"accent", t.accent()},
            {"danger", t.danger()}, {"success", t.success()}, {"warning", t.warning()}};
        for (const auto &[name, c] : text)
            for (const QColor &g : grounds)
                QVERIFY2(Theme::contrast(c, g) >= Theme::TextContrast,
                         qPrintable(QStringLiteral("%1 %2 on %3: %4:1").arg(name, c.name(), g.name())
                                        .arg(Theme::contrast(c, g), 0, 'f', 2)));
        for (const QColor &g : grounds)
            QVERIFY2(Theme::contrast(t.focus(), g) >= Theme::UiContrast, qPrintable(t.focus().name()));
        for (const QColor &g : {t.background(), t.surface()})
            QVERIFY2(Theme::contrast(t.border(), g) >= Theme::UiContrast, qPrintable(t.border().name()));
        QVERIFY(Theme::contrast(t.onAccent(), t.accentFill()) >= Theme::TextContrast);
        QVERIFY(Theme::contrast(t.onDanger(), t.dangerFill()) >= Theme::TextContrast);
        QVERIFY(Theme::contrast(t.onFocus(), t.focus()) >= Theme::TextContrast);
    }

    // Every theme Omarchy ships, when this machine has them (skipped in CI).
    void accessibleOnInstalledThemes()
    {
        const QDir themes(QStringLiteral("/usr/share/omarchy/themes"));
        const QStringList names = themes.entryList(QDir::Dirs | QDir::NoDotAndDotDot);
        if (names.isEmpty())
            QSKIP("Omarchy themes not installed");
        for (const QString &n : names) {
            if (!QFileInfo::exists(themes.filePath(n + "/colors.toml")))
                continue;
            Theme t({themes.filePath(n)});
            for (const QColor &g : {t.background(), t.surface(), t.selection(), t.hover()})
                for (const QColor &c : {t.foreground(), t.muted(), t.accent(), t.danger(), t.success(), t.warning()})
                    QVERIFY2(Theme::contrast(c, g) >= Theme::TextContrast,
                             qPrintable(QStringLiteral("%1: %2 on %3").arg(n, c.name(), g.name())));
            QVERIFY2(Theme::contrast(t.onAccent(), t.accentFill()) >= Theme::TextContrast, qPrintable(n));
            QVERIFY2(Theme::contrast(t.onFocus(), t.focus()) >= Theme::TextContrast, qPrintable(n));
            for (const QColor &g : {t.background(), t.surface(), t.selection(), t.hover()})
                QVERIFY2(Theme::contrast(t.focus(), g) >= Theme::UiContrast, qPrintable(n + ": focus " + t.focus().name()));
        }
    }

    void noOmarchyUsesDefaults()
    {
        Theme t({"/does/not/exist"});
        QVERIFY(t.dark());
        QVERIFY(t.accent().isValid());
        QVERIFY(t.name().isEmpty());
    }

    void reloadsWhenThemeChanges()
    {
        QTemporaryDir tmp;
        const QString dir = tmp.path() + "/current/theme";
        write(dir + "/colors.toml", "mode = \"dark\"\naccent = \"#eeeeee\"\n");
        Theme t({dir});
        QSignalSpy spy(&t, &Theme::changed);

        // Like Omarchy does: replaces the whole directory.
        QDir(dir).removeRecursively();
        write(dir + "/colors.toml", "mode = \"dark\"\naccent = \"#dddddd\"\n");
        QTRY_COMPARE_WITH_TIMEOUT(t.accent(), QColor("#dddddd"), 3000);
        QVERIFY(spy.count() >= 1);
    }
};

QTEST_MAIN(TestTheme)
#include "tst_theme.moc"
