package tree_sitter_test

import (
	"fmt"
	"math/rand"

	. "github.com/tree-sitter/go-tree-sitter"
)

type testEdit struct {
	insertedText  []byte
	position      uint
	deletedLength uint
}

func performEdit(tree *Tree, input *[]byte, edit *testEdit) (InputEdit, error) {
	startByte := edit.position
	oldEndByte := edit.position + edit.deletedLength
	newEndByte := edit.position + uint(len(edit.insertedText))

	startPosition, err := positionForOffset(*input, startByte)
	if err != nil {
		return InputEdit{}, err
	}

	oldEndPosition, err := positionForOffset(*input, oldEndByte)
	if err != nil {
		return InputEdit{}, err
	}

	newInput := make([]byte, 0, len(*input)-int(edit.deletedLength)+len(edit.insertedText))
	newInput = append(newInput, (*input)[:startByte]...)
	newInput = append(newInput, edit.insertedText...)
	newInput = append(newInput, (*input)[oldEndByte:]...)
	*input = newInput

	newEndPosition, err := positionForOffset(*input, newEndByte)
	if err != nil {
		return InputEdit{}, err
	}

	inputEdit := InputEdit{
		StartByte:      startByte,
		OldEndByte:     oldEndByte,
		NewEndByte:     newEndByte,
		StartPosition:  startPosition,
		OldEndPosition: oldEndPosition,
		NewEndPosition: newEndPosition,
	}
	tree.Edit(&inputEdit)
	return inputEdit, nil
}

func positionForOffset(input []byte, offset uint) (Point, error) {
	if offset > uint(len(input)) {
		return Point{}, fmt.Errorf("failed to address an offset: %d", offset)
	}

	var result Point
	var last uint

	for i := uint(0); i < offset; i++ {
		if input[i] == '\n' {
			result.Row++
			last = i
		}
	}

	if result.Row > 0 {
		result.Column = uint(offset - last - 1)
	} else {
		result.Column = uint(offset)
	}

	return result, nil
}

func invertEdit(input []byte, edit *testEdit) *testEdit {
	position := edit.position
	removedContent := input[position : position+edit.deletedLength]
	return &testEdit{
		position:      position,
		deletedLength: uint(len(edit.insertedText)),
		insertedText:  removedContent,
	}
}

func getRandomEdit(rand *rand.Rand, input []byte) testEdit {
	choice := rand.Intn(10)
	if choice < 2 {
		// Insert text at end
		insertedText := randWords(rand, 3)
		return testEdit{
			position:      uint(len(input)),
			deletedLength: 0,
			insertedText:  insertedText,
		}
	} else if choice < 5 {
		// Delete text from the end
		deletedLength := uint(rand.Intn(30))
		if deletedLength > uint(len(input)) {
			deletedLength = uint(len(input))
		}
		return testEdit{
			position:      uint(len(input)) - deletedLength,
			deletedLength: deletedLength,
			insertedText:  []byte{},
		}
	} else if choice < 8 {
		// Insert at a random position
		position := uint(rand.Intn(len(input)))
		wordCount := 1 + rand.Intn(3)
		insertedText := randWords(rand, wordCount)
		return testEdit{
			position:      position,
			deletedLength: 0,
			insertedText:  insertedText,
		}
	} else {
		// Replace at random position
		position := uint(rand.Intn(len(input)))
		deletedLength := uint(rand.Intn(len(input) - int(position)))
		wordCount := 1 + rand.Intn(3)
		insertedText := randWords(rand, wordCount)
		return testEdit{
			position:      position,
			deletedLength: deletedLength,
			insertedText:  insertedText,
		}
	}
}

var operators = []byte{'+', '-', '<', '>', '(', ')', '*', '/', '&', '|', '!', ',', '.', '%'}

func randWords(rand *rand.Rand, maxCount int) []byte {
	var result []byte
	wordCount := rand.Intn(maxCount)
	for i := 0; i < wordCount; i++ {
		if i > 0 {
			if rand.Intn(5) == 0 {
				result = append(result, '\n')
			} else {
				result = append(result, ' ')
			}
		}
		if rand.Intn(3) == 0 {
			index := rand.Intn(len(operators))
			result = append(result, operators[index])
		} else {
			for j := 0; j < rand.Intn(8); j++ {
				result = append(result, byte(rand.Intn(26)+'a'))
			}
		}
	}
	return result
}
