package store

import "strings"

// stopwords are function words that carry no search signal: almost every
// chunk contains them, so in lexical search they only promote noise.
// Russian and English, the two languages of this corpus.
var stopwords = func() map[string]bool {
	const list = `
		и в во не что он на я с со как а то все она так его но да ты к у же
		вы за бы по только ее её мне было вот от меня еще ещё нет о из ему
		теперь когда даже ну вдруг ли если уже или ни быть был него до вас
		нибудь опять уж вам ведь там потом себя ничего ей может они тут где
		есть надо ней для мы тебя их чем была сам чтоб без будто чего раз
		тоже себе под будет ж тогда кто этот того потому этого какой совсем
		ним здесь этом один почти мой тем чтобы нее неё сейчас были куда
		зачем всех можно при два об другой хоть после над больше тот через
		эти нас про всего них какая много разве три эту моя хорошо свою
		этой перед иногда лучше чуть том нельзя такой им более всегда
		конечно всю между это собой собою мной мною тобой тобою нами вами
		ними ею тебе нам вам очень также
		a an the and or but if of at by for with about into to from in on
		is are was were be been being have has had do does did will would
		could should shall may might must can i you he she it we they them
		me my his her its our their your this that these those not no yes
		than then so such when where which who whom whose why how what
	`
	m := map[string]bool{}
	for _, w := range strings.Fields(list) {
		m[w] = true
	}
	return m
}()
