package CloudForest

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strings"
)

//FeatureMatrix contains a slice of Features and a Map to look of the index of a feature
//by its string id.
type FeatureMatrix struct {
	Data       []Feature
	Map        map[string]int
	CaseLabels []string
}

//WriteCases writes a new feature matrix with the specified cases to the the provided writer.
func (fm *FeatureMatrix) WriteCases(w io.Writer, cases []int) (err error) {
	vals := make([]string, 0, len(cases)+1)

	//print header
	vals = append(vals, ".")
	for i := 0; i < len(cases); i++ {
		vals = append(vals, fm.CaseLabels[cases[i]])
	}
	_, err = fmt.Fprintln(w, strings.Join(vals, "\t"))
	if err != nil {
		return
	}

	for i := 0; i < len(fm.Data); i++ {
		vals = vals[0:0]
		vals = append(vals, fm.Data[i].GetName())
		for j := 0; j < len(cases); j++ {
			vals = append(vals, fm.Data[i].GetStr(cases[j]))
		}

		_, err = fmt.Fprintln(w, strings.Join(vals, "\t"))
		if err != nil {
			return
		}

	}

	return

}

/*
BestSplitter finds the best splitter from a number of candidate features to slit on by looping over
all features and calling BestSplit.

leafSize specifies the minimum leafSize that can be be produced by the split.

Vet specifies weather feature splits should be penalized with a randomized version of themselves.

allocs contains pointers to reusable structures for use while searching for the best split and should
be initialized to the proper size with NewBestSplitAlocs.
*/
func (fm *FeatureMatrix) BestSplitter(target Target,
	cases *[]int,
	candidates *[]int,
	oob *[]int,
	leafSize int,
	vet bool,
	evaloob bool,
	allocs *BestSplitAllocs) (s *Splitter, impurityDecrease float64) {

	impurityDecrease = minImp

	var f, bestF *Feature
	var inerImp float64
	var vetImp float64
	var split, bestSplit interface{}

	if vet {
		target.(Feature).CopyInTo(allocs.ContrastTarget.(Feature))
	}

	parentImp := target.Impurity(cases, allocs.Counter)

	for _, i := range *candidates {
		f = &fm.Data[i]
		split, inerImp = (*f).BestSplit(target, cases, parentImp, leafSize, allocs)

		if evaloob && inerImp > minImp && inerImp > impurityDecrease {
			spliter := (*f).DecodeSplit(split)
			l, r, m := spliter.Split(fm, *oob)
			inerImp = target.Impurity(oob, allocs.Counter) - target.SplitImpurity(&l, &r, &m, allocs)
		}

		if vet && inerImp > minImp && inerImp > impurityDecrease {
			casept := cases
			if evaloob {
				casept = oob
			}

			allocs.ContrastTarget.(Feature).ShuffleCases(casept)
			_, vetImp = (*f).BestSplit(allocs.ContrastTarget, casept, parentImp, leafSize, allocs)
			inerImp = inerImp - vetImp
		}

		if inerImp > minImp && inerImp > impurityDecrease {
			bestF = f
			impurityDecrease = inerImp
			bestSplit = split
		}

	}
	if impurityDecrease > minImp {
		s = (*bestF).DecodeSplit(bestSplit)
	}
	return
}

/*
AddContrasts appends n artificial contrast features to a feature matrix. These features
are generated by randomly selecting (with replacement) an existing feature and
creating a shuffled copy named featurename:SHUFFLED.

These features can be used as a contrast to evaluate the importance score's assigned to
actual features.
*/
func (fm *FeatureMatrix) AddContrasts(n int) {
	nrealfeatures := len(fm.Data)
	for i := 0; i < n; i++ {

		//generate a shuffled copy
		orig := fm.Data[rand.Intn(nrealfeatures)]
		fake := orig.ShuffledCopy()

		fm.Map[fake.GetName()] = len(fm.Data)

		fm.Data = append(fm.Data, fake)

	}
}

/*
ContrastAll adds shuffled copies of every feature to the feature matrix. These features
are generated by randomly selecting (with replacement) an existing feature and
creating a shuffled copy named featurename:SHUFFLED.

These features can be used as a contrast to evaluate the importance score's assigned to
actual features. ContrastAll is particularly useful vs AddContrast when one wishes to
identify [pseudo] unique identifiers that might lead to over fitting.
*/
func (fm *FeatureMatrix) ContrastAll() {
	nrealfeatures := len(fm.Data)
	for i := 0; i < nrealfeatures; i++ {

		fake := fm.Data[i].ShuffledCopy()

		fm.Map[fake.GetName()] = len(fm.Data)

		fm.Data = append(fm.Data, fake)

	}
}

/*
ImputeMissing imputes missing values in all features to the mean or mode of the feature.
*/
func (fm *FeatureMatrix) ImputeMissing() {
	for _, f := range fm.Data {
		f.ImputeMissing()
	}
}

//LoadCases will load data stored case by case from a cvs reader into a
//feature matrix that has allready been filled with the coresponding empty
//features. It is a lower level method generally called after inital setup to parse
//a fm, arff, csv etc.
func (fm *FeatureMatrix) LoadCases(data *csv.Reader, rowlabels bool) {
	count := 0
	for {
		record, err := data.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Print("Error:", err)
			break
		}

		caselabel := fmt.Sprintf("%v", count)
		if rowlabels {
			caselabel = record[0]
			record = record[1:]
		}
		fm.CaseLabels = append(fm.CaseLabels, caselabel)

		for i, v := range record {
			fm.Data[i].Append(v)
		}

		count++
	}

}

//Parse an AFM (annotated feature matrix) out of an io.Reader
//AFM format is a tsv with row and column headers where the row headers start with
//N: indicating numerical, C: indicating categorical or B: indicating boolean
//For this parser features without N: are assumed to be categorical
func ParseAFM(input io.Reader) *FeatureMatrix {
	data := make([]Feature, 0, 100)
	lookup := make(map[string]int, 0)
	tsv := csv.NewReader(input)
	tsv.Comma = '\t'
	headers, err := tsv.Read()
	if err == io.EOF {
		return &FeatureMatrix{data, lookup, headers[1:]}
	} else if err != nil {
		log.Print("Error:", err)
		return &FeatureMatrix{data, lookup, headers[1:]}
	}
	headers = headers[1:]

	if len(headers[0]) > 1 {
		sniff := headers[0][:2]
		if sniff == "N:" || sniff == "C:" || sniff == "B:" {
			//features in cols

			for i, label := range headers {
				if label[:2] == "N:" {
					data = append(data, &DenseNumFeature{
						make([]float64, 0, 0),
						make([]bool, 0, 0),
						label,
						false})
				} else {
					data = append(data, &DenseCatFeature{
						&CatMap{make(map[string]int, 0),
							make([]string, 0, 0)},
						make([]int, 0, 0),
						make([]bool, 0, 0),
						label,
						false,
						false})
				}
				lookup[label] = i

			}

			fm := &FeatureMatrix{data, lookup, make([]string, 0, 0)}
			fm.LoadCases(tsv, true)
			return fm
		}
	}

	//features in rows
	count := 0
	for {
		record, err := tsv.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Print("Error:", err)
			break
		}
		data = append(data, ParseFeature(record))
		lookup[record[0]] = count
		count++
	}
	return &FeatureMatrix{data, lookup, headers}
}

//LoadAFM loads a, possible zipped, FeatureMatrix specified by filename
func LoadAFM(filename string) (fm *FeatureMatrix, err error) {

	r, err := zip.OpenReader(filename)
	if err == nil {
		rc, err := r.File[0].Open()
		if err == nil {
			fm = ParseAFM(rc)
			rc.Close()
			return fm, err
		}
	}

	datafile, err := os.Open(filename)
	if err != nil {
		return
	}

	switch {
	case strings.HasSuffix(filename, ".arff"):
		fm = ParseARFF(datafile)
	case strings.HasSuffix(filename, ".libsvm"):
		fm = ParseLibSVM(datafile)
	default:
		fm = ParseAFM(datafile)
	}

	datafile.Close()
	return
}

//ParseFeature parses a Feature from an array of strings and a capacity
//capacity is the number of cases and will usually be len(record)-1 but
//but doesn't need to be calculated for every row of a large file.
//The type of the feature us inferred from the start of the first (header) field
//in record:
//"N:"" indicating numerical, anything else (usually "C:" and "B:") for categorical
func ParseFeature(record []string) Feature {
	capacity := len(record)
	switch record[0][0:2] {
	case "N:":
		f := &DenseNumFeature{
			nil,
			make([]bool, 0, capacity),
			record[0],
			false}
		f.NumData = make([]float64, 0, capacity)

		for i := 1; i < len(record); i++ {
			f.Append(record[i])

		}
		return f

	default:
		f := &DenseCatFeature{
			&CatMap{make(map[string]int, 0),
				make([]string, 0, 0)},
			nil,
			make([]bool, 0, capacity),
			record[0],
			false,
			false}
		f.CatData = make([]int, 0, capacity)
		for i := 1; i < len(record); i++ {
			f.Append(record[i])

		}
		return f
	}

}
