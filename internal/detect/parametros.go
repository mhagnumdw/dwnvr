// Package detect acha os ONSETS - os instantes em que alguma coisa mudou o
// bastante para valer uma olhada cara - a partir do tamanho de cada quadro,
// que o recorder já tem na mão e hoje joga fora.
//
// O desenho tem TRÊS peças separadas, e a separação é o que faz trocar de
// função ser trocar uma string na configuração:
//
//	quadro -> [Score] -> destaque -> [Mecanismo] -> onset -> [Detector]
//	          a função   um número    quem decide    o que    o que era
//	          estatística sem unidade  disparar       acorda
//	                                  e onde olhar
//
// Entre o Mecanismo e o Detector fica o Recortador, que guarda o GOP corrente
// e corta o pedaço de vídeo no quadro que o Mecanismo mandou olhar.
//
// O Score NÃO decide nada. Ele só responde "o quanto este quadro destoa", em
// unidades do ruído da PRÓPRIA câmera. É por isso que o nível de sensibilidade
// significa a mesma coisa numa câmera silenciosa e numa cheia de ruído de
// infravermelho: o que muda entre elas é a régua, não o número.
//
// Duas propriedades não são detalhe, são requisito: O(1) por quadro e ZERO
// alocação no caminho quente. Isto roda dentro do recorder, para todas as
// câmeras, num hardware do porte de um Orange Pi Zero 3.
//
// Tudo aqui foi medido em gravações reais antes de ser escrito. O que a
// detecção faz, e por quê, está em docs/deteccao.md.
package detect

// Este arquivo guarda TODOS os números do pacote. Número solto no meio do
// código é número que ninguém acha para ajustar, e cada um destes tem o motivo
// escrito ao lado: ou saiu de uma medição, ou é uma escolha de desenho.

// --- os cinco níveis de sensibilidade ---------------------------------------

// NivelPadrao é o 4 (alto). No Orange Pi Zero 3, com o modelo do dwnvr-detect,
// ele custa ~9% de um núcleo POR CÂMERA (50 olhadas por hora, de ~6,5 s cada)
// e entrega um terço das chegadas com a família certa na timeline. O nível 5
// dobra esse custo, e com muitas câmeras numa placa de quatro núcleos deixa de
// caber ao lado da gravação.
const NivelPadrao = 4

// NivelMin e NivelMax são a faixa que a UI oferece.
const (
	NivelMin = 1
	NivelMax = 5
)

// OnsetsPorHora é o CUSTO de cada nível, por câmera. É o que o usuário paga:
// cada onset é uma detecção, e uma detecção é o gasto de CPU inteiro da
// feature.
//
// O número é por câmera: com dez câmeras no nível 4 não são 50 detecções por
// hora, são 500.
//
// Ele é a régua COMUM aos dois mecanismos, e é isso que os torna comparáveis:
// no mesmo nível 4 os dois gastam 50 detecções por hora, e o `kleinberg-p`
// entrega 41,5% das chegadas onde o `periodico` entrega 15,1%.
var OnsetsPorHora = [NivelMax + 1]float64{0, 6, 12, 25, 50, 100}

// LimiarKleinbergP é o limiar de abertura que faz o `kleinberg-p` custar
// exatamente o que o nível manda. Foi medido sobre centenas de horas de
// gravação de várias câmeras, com UM limiar só para todas: é o destaque que
// cada câmera tem que atingir, em unidades do próprio ruído, e não um número
// por câmera.
//
// Estes números pertencem aos parâmetros do score logo abaixo. Mexer na meia
// vida, no fator ou no custo de troca sem recalibrar aqui não quebra nada de
// forma visível: só faz os cinco níveis passarem a significar outra coisa, em
// silêncio. Recalibrar é medir de novo, sobre gravações reais, qual limiar dá
// cada custo de OnsetsPorHora.
var LimiarKleinbergP = [NivelMax + 1]float64{0, 15.8246, 11.2852, 7.6377, 4.9695, 2.1312}

// --- o gatilho de histerese -------------------------------------------------

// FracaoLimiarBaixo é quanto do limiar de abrir vira o limiar de fechar. Sem
// os dois limiares, uma pessoa atravessando o quintal faz o sinal cruzar a
// linha vinte vezes e o marcador vira vinte lasquinhas em vez de uma barra.
const FracaoLimiarBaixo = 0.4

// SegundosParaFechar é quanto tempo o destaque precisa ficar abaixo do limiar
// baixo para o intervalo fechar. A pessoa que para um instante não corta o
// marcador em dois.
const SegundosParaFechar = 3.0

// LacunaMs é a folga a partir da qual dois quadros seguidos deixam de ser
// seguidos: acima disso houve buraco de gravação, e o que veio antes não vale
// mais para a estatística.
//
// São os mesmos 2 s que a API usa para costurar faixas contíguas da timeline
// (gapTolerance, em internal/api/recordings.go), e os mesmos que marcaram os
// buracos nas gravações em que os limiares foram medidos. Ter que ser o mesmo
// número não é elegância: é o que faz a medição valer aqui.
const LacunaMs = 2000

// --- quando olhar -----------------------------------------------------------

// JanelaDoPicoMs é quanto tempo depois do onset o mecanismo procura o quadro a
// olhar: o de maior destaque nesta janela, e não o do próprio onset.
//
// Medido: no nível 4, olhar no pico em vez do onset leva `pessoa` de 9,0% para
// 12,2% das chegadas e `veiculo` de 30,8% para 38,6%, sem uma detecção a mais.
// As janelas de 2 s e de 3 s empatam dentro do ruído; esperar um tempo FIXO
// depois do onset, em vez do pico, piora - o objeto já saiu.
//
// O preço é atraso, não CPU: o pedaço só pode ser cortado quando a janela
// fecha.
const JanelaDoPicoMs = 3000

// TetoDoGOPBytes é o maior GOP que o Recortador guarda. Acima disso o resto do
// GOP não é guardado, e o onset que cair nele marca movimento sem ser olhado.
//
// Não é número de afinar, é freio de memória: cada câmera guarda até dois GOPs,
// e numa placa de 1 GB uma câmera sem frame I por minutos não pode crescer um
// buffer sem fim. Câmeras comuns de 28 a 305 kbps, com GOP de 2 a 6 s, ficam
// em ~230 KB no pior caso. Os 4 MB cobrem um 1080p a 8 Mbps com GOP de 4 s, e
// câmera acima disso nem se decodifica em tempo útil numa placa pequena.
const TetoDoGOPBytes = 4 << 20

// --- o score `kleinberg` ----------------------------------------------------

const (
	// MeiaVidaKleinberg é a memória da média e do desvio, em QUADROS e não em
	// segundos, de propósito: as câmeras vão de 10 a 15 fps e a régua tem que
	// ser a mesma em número de amostras.
	MeiaVidaKleinberg = 200.0

	// FatorKleinberg é quantos desvios acima da média o estado "agitado"
	// espera encontrar.
	FatorKleinberg = 3.0

	// CustoTrocaKleinberg é o preço, em log-verossimilhança, de trocar de
	// estado. É ele que impede o detector de ficar pulando entre calmo e
	// agitado a cada quadro.
	CustoTrocaKleinberg = 2.0

	// PisoDesvioLog evita a divisão explodir numa cena parada, em que todos os
	// quadros têm quase o mesmo tamanho e o desvio do log vai a zero.
	//
	// Ele é em unidades de LOG de bytes, e não em bytes. Foram medidas as
	// duas hipóteses de que este piso fosse a alavanca das câmeras que
	// disparam demais, e as duas falharam: este piso nunca encosta nelas, e
	// piso em bytes faz o gatilho disparar MAIS.
	PisoDesvioLog = 0.05

	// QuadrosAntesDoPrimeiroDestaque é quantos quadros o score engole antes de
	// devolver qualquer número diferente de zero. Com menos que isso a média
	// ainda é o primeiro quadro e o z sai absurdo.
	QuadrosAntesDoPrimeiroDestaque = 30

	// CongelaAcima é o destaque a partir do qual o score PARA DE APRENDER.
	//
	// Sem isso a base persegue o próprio evento e o absorve: numa versão
	// anterior, sem congelar, uma câmera subiu 53x acima da base num evento
	// confirmado e o z deu 2,8, que não dispara nem no limiar mais frouxo.
	//
	// Ele é constante INTERNA, e não o limiar do gatilho, de propósito. Se
	// dependesse do gatilho, o destaque mudaria a cada nível de sensibilidade,
	// e a calibração dos limiares teria que ser feita cinco vezes em vez de
	// uma.
	CongelaAcima = 3.0

	// AquecimentoKleinberg é quantos quadros a função precisa ver, depois de
	// cada Zera, antes de o número dela valer. Nesses quadros o mecanismo não
	// dispara: é o mesmo corte aplicado ao medir os limiares.
	AquecimentoKleinberg = 300
)

// MecanismoPadrao é o `kleinberg`, na variante que esconde o frame I. Das
// funções medidas, foi a que mais chegadas pegou pelo mesmo custo: 41,5% no
// nível 4, contra 15,1% de disparar periodicamente. É o que uma câmera
// recém-cadastrada usa.
const MecanismoPadrao = "kleinberg-p"

// --- o que o detector acha, e o que vira marca ------------------------------

// FamiliasPadrao agrupa as classes do modelo nas três famílias que a timeline
// colore. Classe fora daqui não interessa, e é ignorada. É o mesmo mapa sobre o
// qual os cortes abaixo foram medidos.
var FamiliasPadrao = map[string]Familia{
	"person":     Pessoa,
	"bicycle":    Veiculo,
	"car":        Veiculo,
	"motorcycle": Veiculo,
	"bus":        Veiculo,
	"truck":      Veiculo,
	"train":      Veiculo,
	"cat":        Animal,
	"dog":        Animal,
	"horse":      Animal,
	"sheep":      Animal,
	"cow":        Animal,
}

// PisoDoDetector é a menor confiança que o Detector devolve. Fica BEM abaixo do
// corte de propósito: o rastreio aprende com caixa abaixo do corte, e não teria
// como se o detector já as jogasse fora.
//
// Medido: um espantalho, que o modelo confunde com pessoa, sai acima de
// 0,35 em 30% dos quadros diurnos, e acima de 0,20 em 70%. Aprender só com o
// que passa do corte deixaria o rastreio cego para metade das aparições dele.
const PisoDoDetector = 0.20

// CorteDaFamilia é a confiança a partir da qual uma caixa pode virar marca.
//
// 0,40, e não 0,35: medido contra rótulos feitos à mão, ganha em `pessoa` e
// `veiculo` e só perde em `animal`, a família de menor prioridade.
var CorteDaFamilia = map[Familia]float64{
	Pessoa:  0.40,
	Veiculo: 0.40,
	Animal:  0.40,
}

// FaltasParaLiberar é quantas olhadas SEGUIDAS sem ver um lugar ocupado bastam
// para liberá-lo. É a evidência negativa que faz o carro seguinte, na mesma
// vaga da rua, chegar num lugar livre.
//
// 5 e 12 empatam em `pessoa`; 12 perde chegadas de `veiculo` numa câmera
// voltada para a rua.
const FaltasParaLiberar = 5

// IoUDoMesmoLugar é a sobreposição, entre 0 e 1, acima da qual duas caixas da
// mesma família são o mesmo lugar.
//
// 0,7, e não 0,5: com 0,5 uma câmera voltada para a rua perde chegadas de
// veículo (um carro atrás do outro na mesma pista) em troca de 1 `pessoa` falsa
// a menos por câmera por dia.
const IoUDoMesmoLugar = 0.7

// MarcadorPadrao é o que decide o que vira marca numa câmera recém-cadastrada.
const MarcadorPadrao = "rastreio"

// --- a fila até o detector --------------------------------------------------
//
// Os números abaixo saíram de uma reencenação de três dias de gravação de
// várias câmeras dividindo um dwnvr-detect só.

// PedacosPorCamera é quantos pedaços de UMA câmera podem esperar a vez na fila
// ao mesmo tempo. O que está sendo olhado já saiu dela e não conta. O que
// chega com a câmera no limite é descartado: vira só movimento na timeline.
//
// É o único limite da fila, e é o que impede uma câmera agitada de tomar a vez
// das outras: ela só perde o que é DELA. Não há total fixo - o tamanho máximo
// da fila é PedacosPorCamera vezes as câmeras com detecção, e cresce sozinho
// com a instalação. Um total abaixo disso faria justamente o que este limite
// existe para impedir: recusar a marca de uma câmera quieta porque as
// agitadas encheram a fila. A RAM continua limitada, por câmera: cada pedaço
// é até TetoDoGOPBytes, na prática ~230 KB em câmeras comuns.
//
// Por que 2, medido a 3,6 s por olhada (dois núcleos de um Orange Pi Zero 3):
// com 1 por câmera, 3,8% descartados e algumas chegadas de `pessoa` perdidas;
// com 2, 1,9% descartados, nenhuma `pessoa` perdida, e atraso p90 de 16 s
// contra 13 s. Com 3 ou 4 não se ganha mais nada, só atraso: p99 de 40 s e
// 50 s contra 30 s.
const PedacosPorCamera = 2

// PrazoDaOlhadaMs é quanto a fila espera uma resposta do detector antes de
// desistir dela. Uma olhada leva ~6,5 s num núcleo de um Orange Pi Zero 3;
// dez vezes isso é sidecar travado, não sidecar lento.
const PrazoDaOlhadaMs = 60_000

// OlhadasNaMedia é de quantas olhadas recentes saem os tempos que a tela de
// Diagnóstico mostra - quanto uma olhada leva e quanto o pedaço esperou a vez.
// Recentes, e não desde que o dwnvr subiu: a pergunta é "como está agora", e
// uma média de dias não mexeria quando o detector ficasse lento. Com uma
// olhada a cada poucos segundos, 20 são o último minuto ou dois.
const OlhadasNaMedia = 20

// PausaDepoisDeFalhaMs é quanto a fila espera depois de uma olhada que falhou,
// antes de mandar a próxima. Sidecar fora do ar não pode virar laço apertado
// de reconexão; e enquanto ele está fora a fila continua limitada, então a
// gravação não sente nada.
const PausaDepoisDeFalhaMs = 30_000

// Não há REFRATÁRIO. Medido na mesma reencenação: 10 s de refratário por
// câmera perdem 1% das chegadas de `pessoa` com 6,5 s por olhada e 11% com
// 3,4 s; 30 s perdem um terço. Ele descarta justamente a segunda olhada de uma
// cena movimentada. O que impede o mesmo evento de disparar vinte vezes é
// a histerese do Mecanismo.
