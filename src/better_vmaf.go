package src

type BetterVmaf struct {
	VC           VmafComputer
	ChromaWeight int
}

func (bf *BetterVmaf) Run() ([4][]float64, error) {
	var finalScores [4][]float64
	scores, err := bf.VC.Run()
	if err != nil {
		return finalScores, err
	}
	finalScores[0], finalScores[1], finalScores[2] =
		scores[0], scores[1], scores[2]

	if bf.VC.CompareChroma {
		finalScores[3] = make([]float64, len(scores[0]))
		for i := range len(scores[0]) {
			finalScores[3][i] = ((scores[0][i] * float64(bf.ChromaWeight)) +
				scores[1][i] + scores[2][i]) / (float64(bf.ChromaWeight) + 2)
		}
	} else {
		finalScores[3] = scores[0]
	}

	return finalScores, nil
}
