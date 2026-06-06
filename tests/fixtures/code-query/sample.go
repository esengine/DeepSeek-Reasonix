package sample

type User struct {
	Name string
}

type Greeter interface {
	Greet() string
}

func Hello(name string) string {
	return "hi " + name
}

func (u *User) Greet() string {
	return Hello(u.Name)
}
