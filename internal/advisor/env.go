package advisor

import "os"

// lookupEnv は環境変数を読む。テストで差し替えられるよう関数変数にしている。
var lookupEnv = os.Getenv
