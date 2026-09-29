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
            "# comentário\n"
            "accent = \"#3ee8ff\"   # ciano\n"
            "background='#08090a'\n"
            "[secao]\n"
            "lixo sem igual\n");
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
        write(state + "/colors.toml", "mode = \"light\"\naccent = \"#3ee8ff\"\nforeground = \"inválida\"\n");
        write(tmp.path() + "/state/current/theme.name", "sword-art-omarchy\n");

        Theme t({state, config});
        QCOMPARE(t.accent(), QColor("#3ee8ff"));
        QVERIFY(!t.dark());
        QCOMPARE(t.name(), QStringLiteral("sword-art-omarchy"));
        QVERIFY(t.foreground().isValid()); // cor inválida cai no padrão
        QVERIFY(t.background().isValid());
    }

    void noOmarchyUsesDefaults()
    {
        Theme t({"/nao/existe"});
        QVERIFY(t.dark());
        QVERIFY(t.accent().isValid());
        QVERIFY(t.name().isEmpty());
    }

    void reloadsWhenThemeChanges()
    {
        QTemporaryDir tmp;
        const QString dir = tmp.path() + "/current/theme";
        write(dir + "/colors.toml", "accent = \"#111111\"\n");
        Theme t({dir});
        QSignalSpy spy(&t, &Theme::changed);

        // Como o Omarchy faz: substitui o diretório inteiro.
        QDir(dir).removeRecursively();
        write(dir + "/colors.toml", "accent = \"#222222\"\n");
        QTRY_COMPARE_WITH_TIMEOUT(t.accent(), QColor("#222222"), 3000);
        QVERIFY(spy.count() >= 1);
    }
};

QTEST_MAIN(TestTheme)
#include "tst_theme.moc"
