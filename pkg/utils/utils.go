package utils

func CombineMaps(one, two map[string]string) map[string]string {
	newMap := make(map[string]string)
	for k, v := range one {
		newMap[k] = v
	}
	for k, v := range two {
		newMap[k] = v
	}
	return nil
}
