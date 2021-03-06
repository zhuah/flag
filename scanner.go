package flag

import (
	"strings"
)

const (
	argumentFlagSplittable = iota + 1
	argumentFlag
	argumentValue
	argumentPending
)

type argument struct {
	Type  int
	Value string

	// attached value by '=', boolean flag value is optional,
	// it can only use this approach to change it's value as `false`, otherwise
	// it will affects later positional flag and non-flag value parsing.
	AttachValid bool
	Attached    string
}

type scanArgs struct {
	Flags       []argument
	FirstSubset string
	Sets        map[string]*scanArgs
}

type scanner struct {
	SubsetStack []string
	Result      scanArgs
}

func (s *scanner) appendArg(arg argument, isSubset bool) {
	curr := &s.Result
	for _, subset := range s.SubsetStack {
		set := curr.Sets[subset]
		if set == nil {
			set = &scanArgs{}
			if curr.Sets == nil {
				curr.Sets = make(map[string]*scanArgs)
			}
			curr.Sets[subset] = set
		}
		if len(curr.Sets) == 1 {
			curr.FirstSubset = subset
		}
		curr = set
	}
	if !isSubset || len(curr.Flags) == 0 {
		curr.Flags = append(curr.Flags, arg)
	}
}

func (s *scanner) isAlphabet(r rune) bool {
	return ('a' <= r && r <= 'z') || 'A' <= r && r <= 'Z'
}

func (s *scanner) checkSplits(f *FlagSet, rs []rune) (allFlag, firstFlag bool) {
	allFlag = true
	for i, r := range rs {
		isFlag := f.isFlagOrSubset("-" + string(r))
		if isFlag {
			if i == 0 && s.isAlphabet(r) && !s.isAlphabet(rs[i+1]) {
				firstFlag = true
			}
		} else {
			allFlag = false
			break
		}
	}
	return
}

func (s *scanner) stackTopFlagSet(f *FlagSet, stack []string) *FlagSet {
	curr := f
	for _, subset := range stack {
		curr = &curr.subsets[curr.subsetIndexes[subset]]
	}
	return curr
}

func (s *scanner) reverseIterStack(f *FlagSet, fn func(*FlagSet, int) (result, continu bool)) (result bool) {
	for i := len(s.SubsetStack); ; {
		currSet := s.stackTopFlagSet(f, s.SubsetStack[:i])
		result, continu := fn(currSet, i)
		if !continu {
			return result
		}

		i--
		if i < 0 {
			result, _ := fn(nil, i)
			return result
		}
	}
}

func (s *scanner) tryAppendFlagOrSubset(f *FlagSet, arg argument, mustAppend bool) bool {
	return s.reverseIterStack(f, func(currSet *FlagSet, i int) (result, continu bool) {
		if currSet == nil {
			if mustAppend {
				s.appendArg(arg, false)
			}
			return mustAppend, false
		}

		isFlag, isSubset := currSet.isFlag(arg.Value), currSet.isSubset(arg.Value)
		if !isFlag && !isSubset {
			return false, true
		}

		s.SubsetStack = s.SubsetStack[:i]
		if isSubset {
			s.SubsetStack = append(s.SubsetStack, arg.Value)
		}
		arg.Type = argumentFlag
		s.appendArg(arg, isSubset)
		return true, false
	})
}

func (s *scanner) appendSplittable(f *FlagSet, arg argument) {
	flagRunes := []rune(arg.Value[1:])
	s.reverseIterStack(f, func(currSet *FlagSet, i int) (result, continu bool) {
		if currSet == nil {
			arg.Type = argumentFlag
			s.appendArg(arg, false)
			return false, false
		}
		allFlag, firstFlag := s.checkSplits(currSet, flagRunes)
		if allFlag || firstFlag {
			s.SubsetStack = s.SubsetStack[:i]
		}
		if allFlag {
			for i, r := range flagRunes {
				if i == len(flagRunes)-1 {
					s.appendArg(argument{Type: argumentFlag, Value: "-" + string(r), Attached: arg.Attached, AttachValid: arg.AttachValid}, false)
				} else {
					s.appendArg(argument{Type: argumentFlag, Value: "-" + string(r)}, false)
				}
			}
			return false, false
		}
		if firstFlag && !arg.AttachValid {
			s.appendArg(argument{Type: argumentFlag, Value: "-" + string(flagRunes[0])}, false)
			s.appendArg(argument{Type: argumentValue, Value: string(flagRunes[1:])}, false)
			return false, false
		}
		return false, true
	})
}

func (s *scanner) append(f *FlagSet, arg argument) {
	switch arg.Type {
	case argumentValue:
		s.appendArg(arg, false)
	case argumentFlag, argumentPending:
		s.tryAppendFlagOrSubset(f, arg, true)
	case argumentFlagSplittable:
		s.appendSplittable(f, arg)
	}
}

func (s *scanner) canBeSplitBy(arg, sep string) bool {
	index := strings.Index(arg, sep)
	return index > 0 && index <= len(arg)-1
}

func (s *scanner) scanArg(f *FlagSet, args []string, i int) (consumed int) {
	curr := args[i]
	consumed = 1
	switch {
	case i == 0:
		s.append(f, argument{Type: argumentFlag, Value: curr})
	case curr == "--":
		if i != len(args)-1 {
			curr = args[i+1]
			consumed++
			s.append(f, argument{Type: argumentValue, Value: curr})
		}
	case curr == "--*":
		for j := i + 1; j < len(args); j++ {
			curr = args[j]
			s.append(f, argument{Type: argumentValue, Value: curr})
			consumed++
		}
	case strings.HasPrefix(curr, "-") && s.canBeSplitBy(curr[1:], "="):
		secs := strings.SplitN(curr, "=", 2)
		typ := argumentFlag
		if !strings.HasPrefix(secs[0], "--") && len(secs[0]) >= 3 {
			typ = argumentFlagSplittable
		}
		s.append(f, argument{Type: typ, Value: secs[0], Attached: secs[1], AttachValid: true})
	case strings.HasPrefix(curr, "--"):
		s.append(f, argument{Type: argumentFlag, Value: curr})
	case curr != flagNamePositional && s.tryAppendFlagOrSubset(f, argument{Type: argumentPending, Value: curr}, false):
	case curr != "-" && strings.HasPrefix(curr, "-"):
		s.append(f, argument{Type: argumentFlagSplittable, Value: curr})
	default:
		s.append(f, argument{Type: argumentValue, Value: curr})
	}
	return
}

func (s *scanner) scan(f *FlagSet, args []string) {
	for i, l := 0, len(args); i < l; {
		i += s.scanArg(f, args, i)
	}
}
