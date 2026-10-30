package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// portao é uma área válida, para o alerta apontar.
var portao = Area{ID: "k3f9a2", Name: "Portão", Points: quadrado}

// TestAlertaValeNoBoot: o alerta escrito à mão, com uma área que existe, sobe
// como está.
func TestAlertaValeNoBoot(t *testing.T) {
	_, cams, avisos, err := carregaComAvisos(t, "", `[{"id":"cam_a",
		"areas":[{"id":"k3f9a2","name":"Portão","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}],
		"alert":{"enabled":true,"families":["pessoa","veiculo"],"areas":["k3f9a2"]}}]`)
	if err != nil || len(avisos) > 0 {
		t.Fatalf("erro %v, avisos %+v", err, avisos)
	}
	al := cams[0].Alert
	if al == nil || !al.Enabled || !slices.Equal(al.Families, []detect.Familia{detect.Pessoa, detect.Veiculo}) ||
		!slices.Equal(al.Areas, []string{"k3f9a2"}) {
		t.Errorf("alerta %+v", al)
	}
}

// TestAlertaRuimSaiComAviso: a família ou a área que não vale sai com um aviso
// que diz o que é aceito, e o resto do alerta fica ligado. No boot é isso; na
// API, o mesmo aviso vira a recusa.
func TestAlertaRuimSaiComAviso(t *testing.T) {
	casos := map[string]struct {
		alerta   AlertaDaCamera
		campo    string
		aceito   string
		familias []detect.Familia
		areas    []string
	}{
		"família desconhecida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa, "gente"}},
			"alert.families[1]", "pessoa, veiculo, animal", []detect.Familia{detect.Pessoa}, nil,
		},
		"família repetida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa, detect.Pessoa}},
			"alert.families[1]", "não se repita", []detect.Familia{detect.Pessoa}, nil,
		},
		"área que não existe": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2", "sumiu"}},
			"alert.areas[1]", "área desta câmera", []detect.Familia{detect.Pessoa}, []string{"k3f9a2"},
		},
		"área repetida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2", "k3f9a2"}},
			"alert.areas[1]", "não se repita", []detect.Familia{detect.Pessoa}, []string{"k3f9a2"},
		},
	}
	cfg := &Config{}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			antes := AlertaDaCamera{Families: slices.Clone(c.alerta.Families), Areas: slices.Clone(c.alerta.Areas)}
			cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}, Alert: &c.alerta})
			if !slices.Equal(c.alerta.Families, antes.Families) || !slices.Equal(c.alerta.Areas, antes.Areas) {
				t.Error("o ConfereCamera mexeu no alerta de entrada")
			}
			al := cam.Alert
			if !al.Enabled || !slices.Equal(al.Families, c.familias) || !slices.Equal(al.Areas, c.areas) {
				t.Errorf("ficou %+v, esperava ligado com %v e %v", al, c.familias, c.areas)
			}
			if len(avisos) != 1 {
				t.Fatalf("%d avisos: %+v", len(avisos), avisos)
			}
			a := avisos[0]
			if a.Camera != "cam_a" || a.Campo != c.campo || !strings.Contains(a.Aceito, c.aceito) {
				t.Errorf("aviso %+v, esperava %s com %q", a, c.campo, c.aceito)
			}
		})
	}
}

// TestAlertaSemFamiliaDesliga: ligado e sem família que valha, o alerta não
// tem o que avisar, e fica desligado.
func TestAlertaSemFamiliaDesliga(t *testing.T) {
	for nome, familias := range map[string][]detect.Familia{
		"nenhuma":      nil,
		"só inválidas": {"gente"},
	} {
		t.Run(nome, func(t *testing.T) {
			cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a",
				Alert: &AlertaDaCamera{Enabled: true, Families: familias}})
			if cam.Alert.Enabled {
				t.Error("o alerta ficou ligado sem família")
			}
			if len(avisos) == 0 || avisos[len(avisos)-1].Campo != "alert.families" {
				t.Errorf("avisos %+v, esperava terminar em alert.families", avisos)
			}
		})
	}
}

// TestAlertaSemAsAreasDesliga: o alerta que perde todas as áreas fica
// desligado, e não passa a valer na imagem inteira. Vale também para a área
// que existia no arquivo mas saiu por ser inválida.
func TestAlertaSemAsAreasDesliga(t *testing.T) {
	invalida := Area{ID: "k3f9a2", Name: "Portão", Points: quadrado[:2]}
	for nome, areas := range map[string][]Area{
		"área apagada":  nil,
		"área inválida": {invalida},
	} {
		t.Run(nome, func(t *testing.T) {
			cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Areas: areas,
				Alert: &AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2"}}})
			if cam.Alert.Enabled {
				t.Errorf("o alerta ficou ligado: %+v", cam.Alert)
			}
			if len(avisos) == 0 || avisos[len(avisos)-1].Campo != "alert.areas" {
				t.Errorf("avisos %+v, esperava terminar em alert.areas", avisos)
			}
		})
	}
}

// TestAlertaDesligadoGuardaOResto: desligar não apaga as famílias nem as
// áreas, e desligado sem família também vale.
func TestAlertaDesligadoGuardaOResto(t *testing.T) {
	cfg := &Config{}
	guardado := AlertaDaCamera{Families: []detect.Familia{detect.Animal}, Areas: []string{"k3f9a2"}}
	cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}, Alert: &guardado})
	if len(avisos) > 0 || cam.Alert.Enabled || !slices.Equal(cam.Alert.Families, guardado.Families) ||
		!slices.Equal(cam.Alert.Areas, guardado.Areas) {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}

	cam, avisos = cfg.ConfereCamera(Camera{ID: "cam_a", Alert: &AlertaDaCamera{}})
	if len(avisos) > 0 || cam.Alert == nil {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}
}

// TestSemAlertaNaoAvisa: a câmera sem alerta, que é toda câmera de hoje, não
// ganha nada no JSON.
func TestSemAlertaNaoAvisa(t *testing.T) {
	cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}})
	if cam.Alert != nil || len(avisos) > 0 {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}
}

// TestHorariosValemNoBoot: os horários escritos à mão sobem como estão, e os
// que se sobrepõem (12:00 a 13:00 e 12:30 a 14:00 na quarta) valem pela soma,
// sem aviso. Os três primeiros são o exemplo de docs/configuracao.md.
func TestHorariosValemNoBoot(t *testing.T) {
	_, cams, avisos, err := carregaComAvisos(t, "", `[{"id":"cam_a",
		"alert":{"enabled":true,"families":["pessoa"],"schedules":[
			{"days":[3],"from":"00:00","to":"05:15"},
			{"days":[3],"from":"12:00","to":"13:00"},
			{"days":[3],"from":"18:30","to":"23:59"},
			{"days":[1,3],"from":"12:30","to":"14:00"}]}}]`)
	if err != nil || len(avisos) > 0 {
		t.Fatalf("erro %v, avisos %+v", err, avisos)
	}
	al := cams[0].Alert
	want := []HorarioDoAlerta{
		{Days: []time.Weekday{time.Wednesday}, From: "00:00", To: "05:15"},
		{Days: []time.Weekday{time.Wednesday}, From: "12:00", To: "13:00"},
		{Days: []time.Weekday{time.Wednesday}, From: "18:30", To: "23:59"},
		{Days: []time.Weekday{time.Monday, time.Wednesday}, From: "12:30", To: "14:00"},
	}
	if al == nil || !al.Enabled || !reflect.DeepEqual(al.Schedules, want) {
		t.Errorf("alerta %+v", al)
	}
}

// TestHorarioRuimSaiComAviso: o horário que não vale sai inteiro, com um aviso
// que diz o que é aceito, e o alerta fica ligado com os outros. O dia
// repetido não tira o horário: conta uma vez.
func TestHorarioRuimSaiComAviso(t *testing.T) {
	noite := HorarioDoAlerta{Days: []time.Weekday{time.Sunday}, From: "22:00", To: "23:59"}
	casos := map[string]struct {
		ruim   HorarioDoAlerta
		campo  string
		aceito string
		ficam  []HorarioDoAlerta
	}{
		"sem dia": {HorarioDoAlerta{From: "01:00", To: "02:00"},
			"alert.schedules[1].days", "pelo menos um dia", []HorarioDoAlerta{noite}},
		"dia 7": {HorarioDoAlerta{Days: []time.Weekday{1, 7}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[1]", "de 0 (domingo) a 6 (sábado)", []HorarioDoAlerta{noite}},
		"dia negativo": {HorarioDoAlerta{Days: []time.Weekday{-1}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[0]", "de 0 (domingo) a 6 (sábado)", []HorarioDoAlerta{noite}},
		"hora sem o zero": {HorarioDoAlerta{Days: []time.Weekday{1}, From: "9:00", To: "10:00"},
			"alert.schedules[1].from", "HH:MM", []HorarioDoAlerta{noite}},
		"24:00": {HorarioDoAlerta{Days: []time.Weekday{1}, From: "23:00", To: "24:00"},
			"alert.schedules[1].to", "HH:MM", []HorarioDoAlerta{noite}},
		"até igual ao de": {HorarioDoAlerta{Days: []time.Weekday{1}, From: "10:00", To: "10:00"},
			"alert.schedules[1].to", "depois do from 10:00", []HorarioDoAlerta{noite}},
		"atravessa a meia-noite": {HorarioDoAlerta{Days: []time.Weekday{1}, From: "22:00", To: "06:00"},
			"alert.schedules[1].to", "a noite são dois horários", []HorarioDoAlerta{noite}},
		"dia repetido": {HorarioDoAlerta{Days: []time.Weekday{1, 2, 1}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[2]", "não se repita", []HorarioDoAlerta{noite,
				{Days: []time.Weekday{1, 2}, From: "01:00", To: "02:00"}}},
	}
	cfg := &Config{}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			entrada := []HorarioDoAlerta{noite, c.ruim}
			dias := slices.Clone(c.ruim.Days)
			cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Alert: &AlertaDaCamera{Enabled: true,
				Families: []detect.Familia{detect.Pessoa}, Schedules: entrada}})
			if !slices.Equal(entrada[1].Days, dias) {
				t.Error("o ConfereCamera mexeu nos dias de entrada")
			}
			al := cam.Alert
			if !al.Enabled || !reflect.DeepEqual(al.Schedules, c.ficam) {
				t.Errorf("ficou %+v, esperava ligado com %+v", al, c.ficam)
			}
			if len(avisos) != 1 {
				t.Fatalf("%d avisos: %+v", len(avisos), avisos)
			}
			a := avisos[0]
			if a.Camera != "cam_a" || a.Campo != c.campo || !strings.Contains(a.Aceito, c.aceito) {
				t.Errorf("aviso %+v, esperava %s com %q", a, c.campo, c.aceito)
			}
		})
	}
}

// TestHorariosAlemDoTeto: o horário depois do MaxHorariosDoAlerta sai, e os
// primeiros ficam.
func TestHorariosAlemDoTeto(t *testing.T) {
	horarios := make([]HorarioDoAlerta, MaxHorariosDoAlerta+1)
	for i := range horarios {
		minuto := fmt.Sprintf("%02d:%02d", i/60, i%60)
		horarios[i] = HorarioDoAlerta{Days: []time.Weekday{time.Monday}, From: minuto, To: "23:59"}
	}
	cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Alert: &AlertaDaCamera{Enabled: true,
		Families: []detect.Familia{detect.Pessoa}, Schedules: horarios}})
	if !cam.Alert.Enabled || !reflect.DeepEqual(cam.Alert.Schedules, horarios[:MaxHorariosDoAlerta]) {
		t.Errorf("ficaram %d horários, ligado %v", len(cam.Alert.Schedules), cam.Alert.Enabled)
	}
	campo := fmt.Sprintf("alert.schedules[%d]", MaxHorariosDoAlerta)
	if len(avisos) != 1 || avisos[0].Campo != campo {
		t.Errorf("avisos %+v, esperava um em %s", avisos, campo)
	}
}

// TestAlertaSemHorarioQueValhaDesliga: o alerta que perde todos os horários
// fica desligado. Sem eles valeria a qualquer hora, e quem marcou só a noite
// receberia o dia.
func TestAlertaSemHorarioQueValhaDesliga(t *testing.T) {
	cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Alert: &AlertaDaCamera{Enabled: true,
		Families:  []detect.Familia{detect.Pessoa},
		Schedules: []HorarioDoAlerta{{Days: []time.Weekday{1}, From: "22:00", To: "06:00"}}}})
	if cam.Alert.Enabled || len(cam.Alert.Schedules) != 0 {
		t.Errorf("alerta %+v", cam.Alert)
	}
	if len(avisos) != 2 || avisos[1].Campo != "alert.schedules" {
		t.Errorf("avisos %+v, esperava terminar em alert.schedules", avisos)
	}
}

func TestMinutoDoDia(t *testing.T) {
	for s, want := range map[string]int{"00:00": 0, "00:59": 59, "12:30": 750, "23:59": 1439} {
		if m, ok := MinutoDoDia(s); !ok || m != want {
			t.Errorf("MinutoDoDia(%q) = %d, %v; esperava %d", s, m, ok, want)
		}
	}
	for _, s := range []string{"", "9:00", "09:0", "24:00", "12:60", "+1:00", "12-00", "12:000", " 1:00", "1a:00"} {
		if m, ok := MinutoDoDia(s); ok {
			t.Errorf("MinutoDoDia(%q) = %d, esperava recusar", s, m)
		}
	}
}

// TestAlertsEmailSemSecao: quem não configura não paga nada, e não recebe
// aviso por isso.
func TestAlertsEmailSemSecao(t *testing.T) {
	cfg, err := carregaYaml(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if e := cfg.Alerts.Email; len(e.To) != 0 || len(e.Avisos) != 0 {
		t.Errorf("alerts.email %+v, esperado vazio e sem aviso", e)
	}
}

// TestAlertsEmailTiraOQueNaoServe: o endereço inválido sai com aviso, o
// repetido sai calado, e os outros seguem, na ordem escrita.
func TestAlertsEmailTiraOQueNaoServe(t *testing.T) {
	cfg, err := carregaYaml(t, `
smtp:
  host: smtp.gmail.com
  username: dwnvr@exemplo.com
  password: troque-me
alerts:
  email:
    to:
      - fulano@exemplo.com
      - "Ciclana <ciclana@exemplo.com>"
      - " beltrano@exemplo.com "
      - sem-arroba
      - Fulano@Exemplo.com
`)
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Alerts.Email
	if want := []string{"fulano@exemplo.com", "beltrano@exemplo.com"}; !slices.Equal(e.To, want) {
		t.Errorf("to %q, esperado %q", e.To, want)
	}
	if len(e.Avisos) != 2 || !strings.Contains(e.Avisos[0], `"Ciclana <ciclana@exemplo.com>"`) ||
		!strings.Contains(e.Avisos[1], `"sem-arroba"`) {
		t.Errorf("avisos %q, esperado um para o nome na frente e um para o sem arroba", e.Avisos)
	}
}

// TestAlertsEmailSemSMTP: a lista sem a seção smtp não impede o boot, e vira
// aviso. Com a seção recusada, o aviso é o dela, que diz o motivo.
func TestAlertsEmailSemSMTP(t *testing.T) {
	cfg, err := carregaYaml(t, "alerts:\n  email:\n    to: [fulano@exemplo.com]\n")
	if err != nil {
		t.Fatal(err)
	}
	if av := cfg.Alerts.Email.Avisos; len(av) != 1 || !strings.Contains(av[0], "falta a seção smtp") {
		t.Errorf("avisos %q, esperado o da seção smtp que falta", av)
	}

	cfg, err = carregaYaml(t, "smtp:\n  host: smtp.gmail.com\n  security: ssl\n"+
		"alerts:\n  email:\n    to: [fulano@exemplo.com]\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.Recusa == "" || len(cfg.Alerts.Email.Avisos) != 0 {
		t.Errorf("recusa %q e avisos %q; esperado só a recusa", cfg.SMTP.Recusa, cfg.Alerts.Email.Avisos)
	}
}
