package limite

import "time"

const (
	// RajadaDoLogin é quantas tentativas de login seguidas passam antes de o
	// ritmo valer. Quem erra a senha costuma errar duas ou três vezes; cinco
	// cobrem isso com folga, e ninguém que só digitou errado vê a espera.
	// Depois da rajada, as tentativas seguem no ritmo do
	// server.loginAttemptsPerMinute, e as vagas voltam uma a cada intervalo
	// sem tentar.
	RajadaDoLogin = 5

	// LimpezaACada é o intervalo entre as varreduras que apagam os clientes
	// que já recuperaram a rajada inteira. Sem ela, cada endereço que um dia
	// tentou ficaria no mapa para sempre.
	LimpezaACada = time.Minute
)
