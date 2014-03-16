package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/psmithuk/xlsx"
)

var (
	filename       string
	outputfile     string
	testconnection bool
	command        string

	hostname   string
	port       string
	dbname     string
	username   string
	nopassword bool

	columntitles bool
	docuser      string

	showversion bool
)

const VERSION = "0.0.1"

func init() {

	flag.StringVar(&filename, "f", "", "execute command from file (defaults to stdin)")
	flag.StringVar(&outputfile, "o", "", "output file")
	flag.BoolVar(&testconnection, "t", false, "test database connection and exit")
	flag.StringVar(&command, "c", "", "run a single command (ignores other input)")

	flag.StringVar(&hostname, "h", "", "database server host")
	flag.StringVar(&port, "p", "", "database server port")
	flag.StringVar(&dbname, "d", "", "database name to connect to")
	flag.StringVar(&username, "u", "", "username")
	flag.BoolVar(&nopassword, "w", false, "never prompt for password")

	flag.BoolVar(&columntitles, "titles", false, "add row for column titles")
	flag.StringVar(&docuser, "propuser", "", "the username in the xlsx document properties (defaults to current login)")

	flag.BoolVar(&showversion, "version", false, "print version string")
}

func main() {

	var err error

	flag.Usage = usage
	flag.Parse()

	if showversion {
		version()
		return
	}

	if outputfile == "" {
		exitWithError(fmt.Errorf("You must specify an output file name"))
	}

	user, err := user.Current()

	if err != nil {
		exitWithError(fmt.Errorf("unable to get current user: %s", err))
	}

	// unless specified, read the password from the ~/.PGPASS file or prompt
	var password string
	if !nopassword && username != "" {
		password, err = passwordFromPgpass(user)

		// TODO: when the SQL commands are also read from stdin perhaps display a
		// warning
		if err != nil {
			fmt.Print("Enter password: ")
			linereader := bufio.NewReader(os.Stdin)
			b, err := linereader.ReadString('\n')
			if err != nil {
				exitWithError(fmt.Errorf("unable to read password: %s", err.Error()))
			}
			password = string(b)
		}
	}

	// PG connections have useful defaults. Many of these are implemented
	// in lib/pq so we only need to pass options through where specified.

	conn := "sslmode=disable"
	// TODO: support other sslmodes

	if hostname != "" {
		conn = fmt.Sprintf("%s host=%s", conn, hostname)
	}

	if username != "" {
		conn = fmt.Sprintf("%s user=%s", conn, username)
	}

	if username != "" && password != "" {
		conn = fmt.Sprintf("%s password=%s", conn, password)
	}

	if dbname != "" {
		conn = fmt.Sprintf("%s dbname=%s", conn, dbname)
	}

	if port != "" {
		conn = fmt.Sprintf("%s port=%s", conn, port)
	}

	db, err := sql.Open("postgres", conn)
	if err != nil {
		exitWithError(fmt.Errorf("unable to connect to postgres. %s", err))
	}
	defer db.Close()

	// read query from input

	var query string

	if command != "" {
		query = command
	} else {
		var b []byte
		if filename != "" {
			b, err = ioutil.ReadFile(filename)
		} else {
			b, err = ioutil.ReadAll(os.Stdin)
		}

		if err != nil {
			exitWithError(fmt.Errorf("unable to read query: %s", err))
		}

		query = string(b)
	}

	rows, err := db.Query(query)

	if err != nil {
		exitWithError(fmt.Errorf("unable to run query: %s", err))
	}

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		exitWithError(fmt.Errorf("unable to get column names: %s", err))
	}
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))

	sheetColumns := make([]xlsx.Column, len(columns))
	for i, c := range columns {
		sheetColumns[i] = xlsx.Column{Name: c, Width: 10}
	}

	sheet := xlsx.NewSheetWithColumns(sheetColumns)

	// optionally add a row with column titles
	if columntitles {
		titleRow := sheet.NewRow()

		for i, c := range columns {
			titleRow.Cells[i] = xlsx.Cell{xlsx.CellTypeString, c}
		}
		sheet.AppendRow(titleRow)
	}

	// build the data rows
	for rows.Next() {
		sheetRow := sheet.NewRow()

		for i, _ := range columns {
			valuePtrs[i] = &values[i]
		}

		rows.Scan(valuePtrs...)

		for i, _ := range columns {
			val := values[i]
			sheetRow.Cells[i] = CellFromPostgres(val)
		}

		sheet.AppendRow(sheetRow)
	}

	err = sheet.SaveToFile(outputfile)
	if err != nil {
		exitWithError(fmt.Errorf("unable to get save xlsx sheet: %s", err))
	}
}

// Convert a postgres value to a cell, inferring the cell format from the
// database/sql type returned by the pg driver
func CellFromPostgres(v interface{}) xlsx.Cell {
	if v == nil {
		return xlsx.Cell{xlsx.CellTypeString, ""}
	}
	switch v.(type) {
	case ([]uint8):
		s := string(v.([]uint8))
		// string-like values prefixed with one or more
		// zeroes are likely to be UPCs or other codes and should not
		// be converted to numbers
		if len(s) > 0 && (strings.Contains(s, ".") || s[0] != '0') {
			_, err := strconv.ParseFloat(s, 10)
			if err == nil {
				return xlsx.Cell{xlsx.CellTypeNumber, s}
			}
		}
		return xlsx.Cell{xlsx.CellTypeString, s}
	case (bool):
		if v.(bool) {
			return xlsx.Cell{xlsx.CellTypeString, "Y"}
		} else {
			return xlsx.Cell{xlsx.CellTypeString, "N"}
		}
	case (int64):
		return xlsx.Cell{xlsx.CellTypeNumber, fmt.Sprintf("%d", v)}
	case (float64):
		return xlsx.Cell{xlsx.CellTypeNumber, fmt.Sprintf("%f", v)}
	case (time.Time):
		return xlsx.Cell{xlsx.CellTypeDatetime, fmt.Sprintf("%s", v.(time.Time).Format(time.RFC3339))}
	default:
		return xlsx.Cell{xlsx.CellTypeString, fmt.Sprintf("%s", v)}
	}
}

func passwordFromPgpass(user *user.User) (p string, err error) {

	pgpassfilename := fmt.Sprintf("%s/.pgpass", user.HomeDir)
	file, err := os.Open(pgpassfilename)

	if err != nil {
		return "", err
	}

	reader := csv.NewReader(file)
	reader.Comma = ':'
	reader.Comment = '#'
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = 5

	records, err := reader.ReadAll()
	if err != nil {
		return "", err
	}

	// Row format of pgpass file is "host:port:db:user:pass"

	for _, record := range records {
		if record[0] == hostname &&
			record[1] == port &&
			record[2] == dbname &&
			record[3] == username {
			return record[4], nil
		}
	}

	return "", fmt.Errorf("Password for connection not found in %s", filename)
}

// display usage message
func usage() {
	fmt.Fprintf(os.Stderr, "usage: pg2xlsx [flags]\n")
	flag.PrintDefaults()
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}

// print application version
func version() {
	fmt.Printf("v%s\n", VERSION)
}
