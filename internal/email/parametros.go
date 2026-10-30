package email

import "time"

// Os números do envio de e-mail. Trocar um deles é trocar a linha aqui.
const (
	// PrazoDoEnvio é o máximo que um envio espera, da conexão à resposta da
	// mensagem. A folga cobre o servidor lento; quem espera é a tela, com o
	// admin olhando.
	PrazoDoEnvio = 20 * time.Second

	// LinhaDoBase64 é o tamanho da linha do anexo em base64: o máximo da RFC
	// 2045, o que todo leitor de e-mail espera. Sem quebra, um quadro de
	// 50 KB viraria uma linha só, e o SMTP recusa linha com mais de 998.
	LinhaDoBase64 = 76
)
