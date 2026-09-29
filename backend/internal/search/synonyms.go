package search

import "strings"

// ptToEn translates common Portuguese search terms into the English used in
// repository descriptions. Only the query is expanded; the index does not change.
// The keys are in Tokens format (lowercase, no accents, no plural).
var ptToEn = map[string][]string{}

func init() {
	pairs := `
		foto:photo fotografia:photography imagem:image imagen:image desenho:drawing pintura:paint
		editor:editor edicao:editing tema:theme papel:wallpaper parede:wallpaper icone:icon
		calendario:calendar agenda:calendar nota:notes anotacao:annotation tarefa:todo documento:document
		planilha:spreadsheet escrita:writing reuniao:meeting
		musica:music audio:audio video:video voz:voice fala:speech gravador:recorder gravacao:recording
		transcricao:transcription ditado:dictation som:sound tocador:player reprodutor:player
		jogo:game jogos:game emulador:emulator
		arquivo:file gerenciador:manager pasta:folder backup:backup disco:disk senha:password
		rede:network navegador:browser correio:mail email:email conversa:chat mensagem:message
		tela:screen captura:screenshot monitor:monitor janela:window area:workspace trabalho:workspace
		teclado:keyboard atalho:shortcut terminal:terminal maquina:machine virtual:virtual
		configuracao:config ajuste:settings sistema:system painel:panel barra:bar
		desenvolvimento:development codigo:code banco:database dado:data
		loja:store aplicativo:app programa:program ferramenta:tool
	`
	for _, p := range strings.Fields(pairs) {
		pt, en, _ := strings.Cut(p, ":")
		for _, k := range Tokens(pt) {
			for _, v := range Tokens(en) {
				if k != v {
					ptToEn[k] = append(ptToEn[k], v)
				}
			}
		}
	}
}
