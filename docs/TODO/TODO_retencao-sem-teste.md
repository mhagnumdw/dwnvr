# TODO - o pacote `retention` não tem teste nenhum

Levantado em 27/09/2026, ao medir a cobertura para um badge no README.

## O que foi medido

```sh
go test ./... -count=1 -cover
```

`internal/retention` sai com **0,0%**: não existe `retention_test.go`, e nenhum
teste de outro pacote o chama. O total do Go, no mesmo dia, era 63,0%.

## Por que importa

A seção Testes do README diz que os testes "cobrem o que quebra em silêncio:
[...] a retenção". Metade disso é verdade:

- **as primitivas de apagar têm teste**, no `internal/store/store_test.go`:
  `TestEvictOldest`, `TestEvictOldestAtravessaDias`, `TestDropDayLevaOsEventosJunto`,
  `TestPurge*`;
- **a política não tem**: qual dos três limites dispara (cota, `maxDays`, disco
  livre mínimo), em que ordem, e de qual câmera sai o que é apagado. É o
  `internal/retention/retention.go`, e é ele que decide perder gravação.

Um erro ali não dá erro nenhum: o disco enche, ou some gravação que não devia,
e só se percebe olhando a timeline dias depois.

## O que testar

Com um `store.Store` num `t.TempDir()` e um relógio controlável:

1. `enforceQuota`: câmera acima da cota perde o mais antigo até caber, e só ela;
2. `enforceMaxDays`: apaga o dia além do limite; `maxDays` zero não apaga nada;
3. `enforceFreeSpace`: com o disco abaixo do mínimo, sai o dia mais antigo de
   **qualquer** câmera cadastrada, ignorando as cotas;
4. a ordem do `Enforce`: cota, idade e, por último, o disco;
5. câmera cadastrada ou alterada com o dwnvr no ar vale na passada seguinte
   (`cameras` é consultado a cada passada, não copiado na subida).

O item 3 depende de quanto o disco tem livre. Para testar sem encher um disco
de verdade, a leitura do espaço livre (`disk_unix.go`) precisa virar algo
trocável no `Manager`.

## Relacionado

[`TODO_retencao-ignora-gravacoes-orfas.md`](TODO_retencao-ignora-gravacoes-orfas.md)
já apontava que implementar aquela correção teria como "custo real escondido"
escrever este teste. Quem fizer um dos dois começa por este.

Enquanto isso não sai, a frase do README precisa dizer "o apagar da retenção",
ou este teste precisa existir.
