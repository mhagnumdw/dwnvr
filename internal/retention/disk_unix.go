//go:build unix

package retention

import (
	"fmt"
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
