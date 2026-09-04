package dao

type invoiceDAO struct{}

var Invoice = invoiceDAO{}

func (invoiceDAO) Insert(id int) (int64, error) {
	return int64(id), nil
}
