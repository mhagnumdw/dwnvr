package recorder

import "time"

// JanelaDeReconexoes é o "recente" das reconexões: a tela de diagnóstico diz
// quantas caíram dentro dela, ao lado do total desde o início da contagem. O
// total sozinho não separa um enlace que piorou agora de um que teve uma noite
// ruim há dias.
const JanelaDeReconexoes = 8 * time.Hour
