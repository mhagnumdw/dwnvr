//go:build unix

package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FreeBytes devolve o espaço livre disponível para um usuário sem privilégios.
//
// Usa Bavail e não Bfree de propósito: parte dos blocos livres é reservada para
// o root, e contar com eles faria o dwnvr achar que ainda há espaço quando já
// não consegue escrever.
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// TotalBytes é a capacidade do sistema de arquivos.
func TotalBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return int64(st.Blocks) * int64(st.Bsize), nil
}

// IsOnSeparateFilesystem diz se path está num sistema de arquivos diferente da
// raiz.
//
// Serve para o caso real de o disco externo desmontar: sem essa checagem a
// gravação continuaria alegremente no cartão SD do sistema, enchendo o rootfs,
// e ninguém perceberia até o Pi parar de funcionar.
//
// A comparação é com "/" e não com o diretório pai de propósito: o caminho
// configurado costuma ser um subdiretório do ponto de montagem
// (/mnt/storage/dwnvr dentro de /mnt/storage), e comparar com o pai reprovaria
// justamente a configuração mais comum.
func IsOnSeparateFilesystem(path string) (bool, error) {
	dev := func(p string) (uint64, error) {
		fi, err := os.Stat(p)
		if err != nil {
			return 0, err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, fmt.Errorf("não foi possível ler o dispositivo de %s", p)
		}
		return uint64(st.Dev), nil
	}

	target, err := dev(filepath.Clean(path))
	if err != nil {
		return false, err
	}
	root, err := dev("/")
	if err != nil {
		return false, err
	}
	return target != root, nil
}
