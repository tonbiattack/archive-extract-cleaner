//go:build !windows

package removal

import "errors"

func MoveToRecycleBin(folder string) error {
	return errors.New("Windowsのごみ箱はこのOSでは利用できません")
}
