package dragontoothmg

type ColorT uint8

const (
	White ColorT = iota
	Black
	NColors
)

func oppColor(color ColorT) ColorT {
	return Black ^ color
}

type CastleRightsT uint8

const (
	Queenside CastleRightsT = iota
	Kingside
	NSides
)

// This just indicates whether castling rights have been lost, not whether
// castling is actually possible.
type CastleRightsFlagsT uint8

func toFlag(side CastleRightsT) CastleRightsFlagsT {
	return CastleRightsFlagsT(1) << side
}

// TODO type BitBoardT uint64

// Each bitboard shall use little-endian rank-file mapping:
// 56  57  58  59  60  61  62  63
// 48  49  50  51  52  53  54  55
// 40  41  42  43  44  45  46  47
// 32  33  34  35  36  37  38  39
// 24  25  26  27  28  29  30  31
// 16  17  18  19  20  21  22  23
// 8   9   10  11  12  13  14  15
// 0   1   2   3   4   5   6   7
// The binary bitboard uint64 thus uses this ordering:
// MSB---------------------------------------------------LSB
// H8 G8 F8 E8 D8 C8 B8 A8 H7 ... A2 H1 G1 F1 E1 D1 C1 B1 A1

// The board type, which uses little-endian rank-file mapping.
type Board struct {
	Colortomove ColorT
	enpassant     uint8 // square id (16-23 or 40-47) where en passant capture is possible
	Halfmoveclock uint8
	castlerights  [NColors]CastleRightsFlagsT
	Fullmoveno    uint16
	Bbs           [NColors]Bitboards // indexed by color
	pieces        [64]Piece // maps position->piece-type
	hash          uint64
}

func (b *Board) ourBitboards() *Bitboards {
	return &b.Bbs[b.Colortomove]
}

func (b *Board) oppBitboards() *Bitboards {
	return &b.Bbs[oppColor(b.Colortomove)]
}

func bitSet(bits uint64, pos uint8) bool {
	return bits & (uint64(1) << pos) != 0
}

// Return true iff the pieces board is consistent with the bitboards, and the (first) bad square otherwise.
func (b *Board) isConsistent() (bool, uint8) {
	// All pieces in the pieces board must match the corresponding bitboard
	for i := uint8(0); i < 64; i++ {
		piece := b.pieces[i]
		pieceOk := true
		switch piece {
		case Nothing:
			pieceOk = bitSet(^(b.Bbs[White][All] | b.Bbs[Black][All]), i)
		case Pawn:
			pieceOk = bitSet((b.Bbs[White][Pawn] | b.Bbs[Black][Pawn]), i)
		case Knight:
			pieceOk = bitSet((b.Bbs[White][Knight] | b.Bbs[Black][Knight]), i)
		case Bishop:
			pieceOk = bitSet((b.Bbs[White][Bishop] | b.Bbs[Black][Bishop]), i)
		case Rook:
			pieceOk = bitSet((b.Bbs[White][Rook] | b.Bbs[Black][Rook]), i)
		case Queen:
			pieceOk = bitSet((b.Bbs[White][Queen] | b.Bbs[Black][Queen]), i)
		case King:
			pieceOk = bitSet((b.Bbs[White][King] | b.Bbs[Black][King]), i)
		default:
			pieceOk = false
		}

		if !pieceOk {
			return false, i
		}

	}
	return true, 0
}

func (b *Board) addPiece(piece Piece, pos uint8, pieceBitboard *uint64, allBitboard *uint64) {
	*pieceBitboard |= (uint64(1) << pos)
	*allBitboard |= (uint64(1) << pos)
	b.pieces[pos] = piece
}

func (b *Board) removePiece(piece Piece, pos uint8, pieceBitboard *uint64, allBitboard *uint64) {
	*pieceBitboard &= ^(uint64(1) << pos)
	*allBitboard &= ^(uint64(1) << pos)
	b.pieces[pos] = Nothing
}

// To square MUST be empty - remove capture piece explicity before calling this
// For promotions the destPiece/destTypeBitboard is not the same as the original piece/pieceBitboard
func (b *Board) movePiece(piece Piece, destPiece Piece, from uint8, to uint8, pieceBitboard *uint64, destTypeBitboard *uint64, allBitboard *uint64) {
	b.removePiece(piece, from, pieceBitboard, allBitboard)
	b.addPiece(destPiece, to, destTypeBitboard, allBitboard)
}

// Return the Zobrist hash value for the board.
// The hash value does NOT change with the turn number, nor the draw move counter.
// All other elements of the Board type affect the hash.
// This function is cheap to call, since the hash is incrementally updated.
func (b *Board) Hash() uint64 {
	//b.hash = recomputeBoardHash(b)
	return b.hash
}

func (b *Board) Enpassant() uint8 {
	return b.enpassant
}

// Castle rights helpers.

// Castling helper functions for all 16 possible scenarios
func (b *Board) canCastle(color ColorT, side CastleRightsT) bool {
	return b.castlerights[color] & toFlag(side) != 0
}

func (b *Board) weCanCastle(side CastleRightsT) bool {
	return b.canCastle(b.Colortomove, side)
}

func (b *Board) oppCanCastle(side CastleRightsT) bool {
	return b.canCastle(oppColor(b.Colortomove), side)
}

func (b *Board) flipCastleRights(color ColorT, side CastleRightsT) {
	b.castlerights[color] ^= toFlag(side)
	b.hash ^= castleRightsZobristC[color][side]
}

func (b *Board) flipOurCastleRights(side CastleRightsT) {
	b.flipCastleRights(b.Colortomove, side)
}

func (b *Board) flipOppCastleRights(side CastleRightsT) {
	b.flipCastleRights(oppColor(b.Colortomove), side)
}

func (b *Board) isWhitePieceAt(pos uint8) bool {
	return b.Bbs[White][All] & (uint64(1) << pos) != 0
}

func (b *Board) isBlackPieceAt(pos uint8) bool {
	return b.Bbs[Black][All] & (uint64(1) << pos) != 0
}

func (b *Board) PieceAt(pos uint8) Piece {
	return b.pieces[pos]
}

// Contains bitboard representations of all the pieces for a side.
type Bitboards [NPiecesWithAll]uint64

// Return the bitboard for the given piece - should really be a table lookup
func (bb *Bitboards) pieceBitboard(piece Piece) *uint64 {
	return &bb[piece]
}

// Data stored inside, from LSB
// 6 bits: destination square
// 6 bits: source square
// 3 bits: promotion

// Move bitwise structure; internal implementation is private.
type Move uint16

func (m *Move) To() uint8 {
	return uint8(*m & 0x3F)
}
func (m *Move) From() uint8 {
	return uint8((*m & 0xFC0) >> 6)
}
// Whether the move involves promoting a pawn.
func (m *Move) Promote() Piece {
	return Piece((*m & 0x7000) >> 12)
}
// Is the move simple - i.e. NOT promo, en-passant or castling
// This is just a hint - if not set then move application will still work.
func (m *Move) IsSimple() bool {
	return (*m & Move(0x8000)) != 0
}
func (m *Move) Setto(s Square) *Move {
	*m = *m & ^(Move(0x3F)) | Move(s)
	return m
}
func (m *Move) Setfrom(s Square) *Move {
	*m = *m & ^(Move(0xFC0)) | (Move(s) << 6)
	return m
}
func (m *Move) Setpromote(p Piece) *Move {
	*m = *m & ^(Move(0x7000)) | (Move(p) << 12)
	return m
}
func (m *Move) Setsimple() *Move {
	*m = *m | Move(0x8000)
	return m
}
func (m *Move) String() string {
	/*return fmt.Sprintf("[from: %v, to: %v, promote: %v]",
	IndexToAlgebraic(Square(m.From())), IndexToAlgebraic(Square(m.To())), m.Promote())*/
	if *m == 0 {
		return "0000"
	}
	result := IndexToAlgebraic(Square(m.From())) + IndexToAlgebraic(Square(m.To()))
	switch m.Promote() {
	case Queen:
		result += "q"
	case Knight:
		result += "n"
	case Rook:
		result += "r"
	case Bishop:
		result += "b"
	default:
	}
	return result
}

// Square index values from 0-63.
type Square uint8

// Piece types; valid in range 0-6, as indicated by the constants for each piece.
type Piece uint8

const (
	Nothing Piece = iota
	Pawn
	Knight // list before bishop for promotion loops
	Bishop
	Rook
	Queen
	King
	NPieces
)

const All = NPieces
const NPiecesWithAll = NPieces + 1

// Move application data
type MoveApplication struct {
	Move Move
	Unapply func()
	FromPieceType Piece
	ToPieceType Piece // Different from fromPieceType only for promotions
	CapturedPieceType Piece // Nothing if this is not a capture
	CaptureLocation uint8
	IsCastling bool
	RookCastleFrom uint8 // Only valid if IsCastling
	RookCastleTo uint8 // Only valid if IsCastling
}

