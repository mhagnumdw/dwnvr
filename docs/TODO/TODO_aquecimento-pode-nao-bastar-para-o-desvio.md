# O aquecimento pode não bastar para o desvio da régua

`AquecimentoKleinberg` são 300 quadros - 20 a 30 s, conforme a câmera. Depois
deles o gatilho passa a disparar. A média da EWMA já assentou aí; o **desvio**
talvez não.

## O que está medido

Montando a fixture de `quadros_reais_test.go`, o mesmo episódio deu destaques
diferentes conforme quanto de cena parada veio ANTES dele:

| início da série | quadros até o evento | maior destaque na pessoa |
|---|---|---|
| 10:43:30 | ~900 | **8,52** |
| 10:41:00 | ~2.400 | 1,61 |
| 10:38:03 | ~4.100 | 1,61 |

O valor que a gravação produziu de verdade é 1,61. Com 900 quadros de cena
parada - três vezes o aquecimento - a régua ainda devolvia 5 vezes mais.

A causa é o `vari` da EWMA: ele sobe de zero, e enquanto está subestimado o z
sai inflado, porque o desvio é o denominador dele.

## O que NÃO está medido

A consequência agregada **não apareceu**. Medindo a taxa de onsets por hora em
função de quantos quadros se passaram desde o último `Zera`
(`detect-test-v5/aquecimento`), sobre 9 câmera-dias:

| quadros desde o Zera | onsets/hora |
|---|---|
| 300 a 600 | 113,8 |
| 600 a 1.200 | 80,5 |
| 1.200 a 2.400 | 93,7 |
| 2.400 a 4.800 | 127,9 |
| 9.600 em diante | 99,4 |

Se o aquecimento curto fizesse a câmera disparar demais logo depois de cada
reconexão, a primeira faixa seria a mais alta e a curva desceria. Ela não é, e
não desce.

Uma explicação possível é que o que importa não é o tempo desde o `Zera`, e sim
quanto de **cena parada** a régua teve antes do primeiro evento - e isso a
medição por faixas não separa. Câmera com muitos buracos também tende a ser
câmera com muito movimento, então as duas coisas se confundem.

## O que fazer

Antes de mexer em `AquecimentoKleinberg`, separar as duas variáveis: medir a
taxa de onsets em função dos quadros de cena PARADA desde o `Zera`, e não do
tempo total. Se o efeito aparecer ali, o conserto provável não é aumentar o
aquecimento (que cegaria a câmera por minutos depois de cada buraco, e buraco
aqui é rotina) e sim **semear o desvio** com um valor típico em vez de zero.
