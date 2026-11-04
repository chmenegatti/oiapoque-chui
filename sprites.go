package main

var carPal = map[byte]RGB{
	'k': {25, 25, 25},    // contorno
	'w': {90, 120, 140},  // vidro
	'b': {230, 180, 40},  // carroceria
	'R': {190, 20, 20},   // lanterna
	'O': {230, 130, 20},  // pisca
	'p': {225, 225, 225}, // placa
	'c': {170, 170, 170}, // para-choque
	't': {12, 12, 12},    // pneu
	'z': {200, 220, 230}, // espuma
	'H': {225, 225, 200}, // farol dianteiro
	'y': {240, 200, 40},  // faixa/letreiro
}

// Traseira do Fiat 147: hatch quadradão, lanternas retangulares.
var spr147 = newSprite(carPal,
	"......kkkkkkkkkkkk......",
	".....kwwwwwwwwwwwwk.....",
	"....kwwwwwwwwwwwwwwk....",
	"...kwwwwwwwwwwwwwwwwk...",
	"..kbbbbbbbbbbbbbbbbbbk..",
	".kbbbbbbbbbbbbbbbbbbbbk.",
	".kRRRbbbbbbbbbbbbbbRRRk.",
	".kRRRbbbbbppppbbbbbRRRk.",
	".kOOObbbbbppppbbbbbOOOk.",
	".kbbbbbbbbbbbbbbbbbbbbk.",
	".kcccccccccccccccccccck.",
	".kkkkkkkkkkkkkkkkkkkkkk.",
	"..ttt..............ttt..",
)

// Carro genérico do tráfego (sedã).
var sprCar = newSprite(carPal,
	"....kkkkkkkkkkkk....",
	"...kwwwwwwwwwwwwk...",
	"..kwwwwwwwwwwwwwwk..",
	".kbbbbbbbbbbbbbbbbk.",
	"kbbbbbbbbbbbbbbbbbbk",
	"kRRbbbbbbppbbbbbbRRk",
	"kbbbbbbbbppbbbbbbbbk",
	"kcccccccccccccccccck",
	".tt..............tt.",
)

// ---------- cenário ----------

var scenePal = map[byte]RGB{
	'g': {28, 100, 38},   // folha escura
	'G': {60, 145, 60},   // folha clara
	'Y': {110, 170, 50},  // folha de bananeira
	't': {95, 65, 38},    // tronco
	'k': {30, 30, 30},    // contorno
	'v': {20, 110, 60},   // placa verde
	'w': {240, 240, 240}, // branco
	'p': {140, 140, 140}, // poste
	'R': {200, 30, 30},   // cobertura do posto
	'O': {240, 180, 30},  // bomba
	'c': {200, 200, 200}, // coluna
	'o': {120, 135, 55},  // folha do cerrado
	'T': {70, 45, 25},    // tronco escuro
	'z': {200, 220, 230}, // espuma
	'b': {40, 90, 170},   // casco (substituído pela cor do barco)
}

var sprArvore = newSprite(scenePal,
	"....gggg....",
	"..ggGgggGg..",
	".gggggGgggg.",
	"gGggggggggGg",
	"gggggGgggggg",
	"gggGgggggGgg",
	".gggggggggg.",
	"..ggggGggg..",
	"....gttg....",
	".....tt.....",
	".....tt.....",
	".....tt.....",
)

var sprBananeira = newSprite(scenePal,
	"Y.....Y....Y",
	".Y....Y...Y.",
	"..YY..Y..Y..",
	"YY..YYYYY..Y",
	"...YYYYYYY..",
	"..Y...t...Y.",
	".Y....t....Y",
	"......t.....",
	"......t.....",
	"......t.....",
)

var sprPlaca = newSprite(scenePal,
	"kkkkkkkkkkkkkkkk",
	"kvvvvvvvvvvvvvvk",
	"kvwwwwvwwwwwwvvk",
	"kvvvvvvvvvvvvvvk",
	"kvvwwwwwwvwwwvvk",
	"kvvvvvvvvvvvvvvk",
	"kkkkkkkkkkkkkkkk",
	".......pp.......",
	".......pp.......",
	".......pp.......",
)

var sprPosto = newSprite(scenePal,
	"RRRRRRRRRRRRRRRRRRRR",
	"wwwwwwwwwwwwwwwwwwww",
	"RRRRRRRRRRRRRRRRRRRR",
	".cc......cc......cc.",
	".cc..O...cc...O..cc.",
	".cc.OOO..cc..OOO.cc.",
	".cc.OOO..cc..OOO.cc.",
	".cc.OOO..cc..OOO.cc.",
)

var sprKm = newSprite(scenePal,
	"www",
	"wkw",
	"www",
	".p.",
	".p.",
)

var sprAcai = newSprite(scenePal,
	"Y...Y...Y",
	".Y..Y..Y.",
	"..YYYYY..",
	"YY.YYY.YY",
	"....t....",
	"....t....",
	"....t....",
	"....t....",
	"....t....",
	"....t....",
)

var sprPequi = newSprite(scenePal,
	"..oooo.ooo..",
	".oooooooooo.",
	"oooo.oooooo.",
	"..ooo..ooo..",
	"....t..t....",
	".....tt.....",
	".....t......",
	".....t......",
)

var sprVaca = newSprite(scenePal,
	"..........kk",
	".wwkwwwkwwkw",
	"wwwwkkwwwwww",
	"wkwwwwwkww..",
	".k.k...k.k..",
)

var sprTronco = newSprite(scenePal,
	".tttttttttttt.",
	"tTtttTttttTttt",
	".tttttttttttt.",
)

var sprBarco = newSprite(scenePal,
	".....wwwwww.....",
	".....wkkkkw.....",
	"...wwwwwwwwww...",
	"..wbbbbbbbbbbw..",
	".wbbbbbbbbbbbbw.",
	"kkkkkkkkkkkkkkkk",
	".kkkkkkkkkkkkkk.",
	"..zzzzzzzzzzzz..",
)

// Placa de limite 80 km/h.
var sprLimite = newSprite(scenePal,
	"..RRRR..",
	".RwwwwR.",
	"RwkwkwwR",
	"RwkwkwwR",
	".RwwwwR.",
	"..RRRR..",
	"...pp...",
	"...pp...",
	"...pp...",
	"...pp...",
)

var sprRadar = newSprite(scenePal,
	"kkkkkk",
	"kwwwwk",
	"kwkkwk",
	"kkkkkk",
	"..pp..",
	"..pp..",
	"..pp..",
	"..pp..",
	"..pp..",
	"..pp..",
)

// Balsa com o 147 no convés.
var sprBalsa = newSprite(carPal,
	"........kkkkkkkk........",
	".......kwwwwwwwwk.......",
	"......kbbbbbbbbbbk......",
	"......kRbbbbbbbbRk......",
	"......kkkkkkkkkkkk......",
	"cccccccccccccccccccccccc",
	"kRRRkkkkkkkkkkkkkkkkRRRk",
	"kkkkkkkkkkkkkkkkkkkkkkkk",
	".zzzzzzzzzzzzzzzzzzzzzz.",
)

// ---------- tráfego: traseiras e frentes ----------

var sprCarFront = newSprite(carPal,
	"....kkkkkkkkkkkk....",
	"...kwwwwwwwwwwwwk...",
	"..kwwwwwwwwwwwwwwk..",
	".kbbbbbbbbbbbbbbbbk.",
	"kHHbbbbkkkkkkbbbbHHk",
	"kbbbbbbkckckkbbbbbbk",
	"kcccccccccccccccccck",
	".tt..............tt.",
)

var sprTruck = newSprite(carPal,
	"kkkkkkkkkkkkkkkkkkkk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbkbbbbbbbbbk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kRRkkkkkkppkkkkkkRRk",
	"kkkkkkkkkkkkkkkkkkkk",
	"cccccccccccccccccccc",
	".ttt.ttt....ttt.ttt.",
)

var sprTruckFront = newSprite(carPal,
	"....kkkkkkkkkkkk....",
	"...kwwwwwwwwwwwwk...",
	"..kwwwwwwwwwwwwwwk..",
	"..kwwwwwwwwwwwwwwk..",
	"..kbbbbbbbbbbbbbbk..",
	"..kbkkkkkkkkkkkkbk..",
	"..kbkckckckckckkbk..",
	"..kHHbbbbbbbbbbHHk..",
	"..kbbbbbbbbbbbbbbk..",
	"..cccccccccccccccc..",
	"...tt..........tt...",
)

var sprBus = newSprite(carPal,
	"kkkkkkkkkkkkkkkkkkkk",
	"kwwwwwwwwwwwwwwwwwwk",
	"kwwwwwwwwwwwwwwwwwwk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbyyyyyyyyyyyyyyyybk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbbbbbbbkkkkbbbbbbbk",
	"kbbbbbbbkkkkbbbbbbbk",
	"kRRbbbbbbppbbbbbbRRk",
	"kkkkkkkkkkkkkkkkkkkk",
	".ttt............ttt.",
)

var sprBusFront = newSprite(carPal,
	"kkkkkkkkkkkkkkkkkkkk",
	"kyyyyyyyyyyyyyyyyyyk",
	"kwwwwwwwwwwwwwwwwwwk",
	"kwwwwwwwwwwwwwwwwwwk",
	"kwwwwwwwwwwwwwwwwwwk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kbbbbbbbbbbbbbbbbbbk",
	"kHHbbbbbbbbbbbbbbHHk",
	"kbbbbbbbbbbbbbbbbbbk",
	"cccccccccccccccccccc",
	".ttt............ttt.",
)
