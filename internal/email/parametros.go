package email

import "time"

// Os números do envio de e-mail. Trocar um deles é trocar a linha aqui.
const (
	// PrazoDoEnvio é o máximo que um envio espera, da conexão à resposta da
	// mensagem. A folga cobre o servidor lento; quem espera é a tela, com o
	// admin olhando.
	PrazoDoEnvio = 20 * time.Second
)
