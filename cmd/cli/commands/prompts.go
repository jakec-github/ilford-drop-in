package commands

import (
	"bufio"
	"fmt"
	"strings"
)

func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptConfirm(reader *bufio.Reader, prompt string) (bool, error) {
	fmt.Print(prompt + " [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}
