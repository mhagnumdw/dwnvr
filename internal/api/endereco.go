package api

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// clienteDe é o endereço de quem fez a requisição, para o limite do login.
//
// Atrás de um proxy, a conexão vem do proxy, igual para todo mundo, e quem
// chegou de verdade está no X-Forwarded-For. Mas o header quem escreve é o
// cliente, e só vale quando a conexão vem de um proxy confiável (o
// server.trustedProxies). Aí ele é lido da direita para a esquerda, pulando os
// proxies confiáveis, e o primeiro endereço fora deles é o cliente: o que está
// à esquerda dele foi escrito por quem não é confiável e pode ser inventado.
//
// O `tailscale serve` e o Caddy da doc substituem o header que recebem, mas
// um proxy que acrescenta ao que veio (o Caddy com trusted_proxies, por
// exemplo) não engana esta leitura.
func clienteDe(r *http.Request, confiaveis []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	cliente, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	cliente = cliente.Unmap()

	confiavel := func(a netip.Addr) bool {
		return slices.ContainsFunc(confiaveis, func(p netip.Prefix) bool { return p.Contains(a) })
	}
	if !confiavel(cliente) {
		return cliente
	}
	var saltos []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		saltos = append(saltos, strings.Split(v, ",")...)
	}
	for _, s := range slices.Backward(saltos) {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil {
			break // o resto da cadeia não é confiável: fica o último bom
		}
		cliente = a.Unmap()
		if !confiavel(cliente) {
			break
		}
	}
	return cliente
}
