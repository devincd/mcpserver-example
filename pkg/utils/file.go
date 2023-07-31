package utils

import "os"

// PathExists 返回文件或者文件夹的FileInfo信息
// 1.如果返回的错误为nil，说明文件或文件夹存在。
// 2.如果返回的错误类型使用errors.Is(err, fs.ErrNotExist)判断为true，说明文件或文件夹不存在
// 3.如果返回的错误为其他类型，则不确定是否存在
// https://studygolang.com/articles/23000
func PathExists(path string) error {
	_, err := os.Stat(path)
	return err
}
