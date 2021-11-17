package autoload

/*
	You can just read the .env file on import just by doing

		import _ "github.com/hoshsadiq/godotenv/autoload"

	And bob's your mother's brother
*/

import "github.com/hoshsadiq/godotenv"

func init() {
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}
}
