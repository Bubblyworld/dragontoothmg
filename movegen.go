// dragontoothmg is a fast chess legal move generator library based on magic bitboards.
package dragontoothmg

// The main Dragontooth move generator file.
// Functions are in this file if (and only if) they are performance-critical
// move generator components, called while actually generating moves in-game.
// (The exception is a few one-line helpers for Move and Board in types.go)

import (
	//"fmt"
	"math/bits"
)

var promoRankBbs = [NColors]uint64 {onlyRank[7], onlyRank[0]}
var doublePushRankBBs = [NColors]uint64 {onlyRank[3], onlyRank[4]}
var pawnPushDirections = [NColors]int {1, -1}
var oneRankBacks = [NColors]int {-8, 8}

// The main API entrypoint. Generates all legal moves for a given board.
func (b *Board) GenerateLegalMoves() []Move {
	moves, _ := b.GenerateLegalMoves2(false)
	return moves
}


// The main API entrypoint. Generates legal moves for a given board,
//   either all moves (onlyCapturesPromosCheckEvasion == false), or
//   limited to captures, promotions, and check evasion for quiescence search.
// Return moves, isInCheck
func (b *Board) GenerateLegalMoves2(onlyCapturesPromosCheckEvasion bool) ([]Move, bool) {
	moves := make([]Move, 0, kDefaultMoveListLength)
	// First, see if we are currently in check. If we are, invoke a special check-
	// evasion move generator.
	ourCol := b.Colortomove
	ourPiecesPtr := &b.Bitboards[ourCol]

	// assumes only one king
	kingLocation := uint8(bits.TrailingZeros64(ourPiecesPtr.Kings))

	kingAttackers, blockerDestinations := b.countAttacks(ourCol == White, kingLocation, 2)
	if kingAttackers >= 2 { // Under multiple attack, we must move the king.
		b.kingPushes(&moves, ourPiecesPtr, everything)
		return moves, true
	}

	// Several move types can work in single check, but we must block the check
	if kingAttackers == 1 {
		// calculate pinned pieces
		pinnedPieces := b.generatePinnedMoves(&moves, blockerDestinations)
		nonpinnedPieces := ^pinnedPieces
		// TODO
		b.pawnPushes(&moves, nonpinnedPieces, blockerDestinations)
		b.pawnCaptures(&moves, nonpinnedPieces, blockerDestinations)
		b.knightMoves(&moves, nonpinnedPieces, blockerDestinations)
		b.rookMoves(&moves, nonpinnedPieces, blockerDestinations)
		b.bishopMoves(&moves, nonpinnedPieces, blockerDestinations)
		b.queenMoves(&moves, nonpinnedPieces, blockerDestinations)
		b.kingPushes(&moves, ourPiecesPtr, everything)
		return moves, true
	}

	allowDest := everything
	// If we're only interested in captures, then limit destinations to opponent pieces
	oppCol := oppColor(ourCol)
	if onlyCapturesPromosCheckEvasion {
		allowDest = b.Bitboards[oppCol].All
	}

	// Then, calculate all the absolutely pinned pieces, and compute their moves.
	// If we are in check, we can only move to squares that block the check.
	pinnedPieces := b.generatePinnedMoves(&moves, allowDest)
	nonpinnedPieces := ^pinnedPieces

	// always generate pawn promos
	promoDest := promoRankBbs[ourCol]
	
	// Finally, compute ordinary moves, ignoring absolutely pinned pieces on the board.
	b.pawnPushes(&moves, nonpinnedPieces, allowDest|promoDest)
	b.pawnCaptures(&moves, nonpinnedPieces, allowDest)
	b.knightMoves(&moves, nonpinnedPieces, allowDest)
	b.rookMoves(&moves, nonpinnedPieces, allowDest)
	b.bishopMoves(&moves, nonpinnedPieces, allowDest)
	b.queenMoves(&moves, nonpinnedPieces, allowDest)
	b.kingMoves(&moves, allowDest, /*includeCastling*/!onlyCapturesPromosCheckEvasion)
	return moves, false
}

// Calculate the available moves for absolutely pinned pieces (pinned to the king).
// We are only allowed to move to squares in allowDest, to block checks.
// Return a bitboard of all pieces that are pinned.
func (b *Board) generatePinnedMoves(moveList *[]Move, allowDest uint64) uint64 {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]
	oppCol := oppColor(ourCol)
	oppPieces := &b.Bitboards[oppCol]

	// TODO naming consistency
	
	// assumes only one king
	ourKingIdx := uint8(bits.TrailingZeros64(ourPieces.Kings))
	allPinnedPieces := uint64(0)
	pawnPushDirection := pawnPushDirections[ourCol]
	doublePushRank := doublePushRankBBs[ourCol]
	ourPromotionRank := promoRankBbs[ourCol]
	
	allPieces := oppPieces.All | ourPieces.All

	// Calculate king moves as if it was a rook.
	// "king targets" includes our own friendly pieces, for the purpose of identifying pins.
	kingOrthoTargets := CalculateRookMoveBitboard(ourKingIdx, allPieces)
	oppRooks := oppPieces.Rooks | oppPieces.Queens
	for oppRooks != 0 { // For each opponent ortho slider
		currRookIdx := uint8(bits.TrailingZeros64(oppRooks))
		oppRooks &= oppRooks - 1
		rookTargets := CalculateRookMoveBitboard(currRookIdx, allPieces) & (^(oppPieces.All))
		// A piece is pinned iff it falls along both attack rays.
		pinnedPiece := rookTargets & kingOrthoTargets & ourPieces.All
		if pinnedPiece == 0 { // there is no pin
			continue
		}
		pinnedPieceIdx := uint8(bits.TrailingZeros64(pinnedPiece))
		sameRank := pinnedPieceIdx/8 == ourKingIdx/8 && pinnedPieceIdx/8 == currRookIdx/8
		sameFile := pinnedPieceIdx%8 == ourKingIdx%8 && pinnedPieceIdx%8 == currRookIdx%8
		if !sameRank && !sameFile {
			continue // it's just an intersection, not a pin
		}
		allPinnedPieces |= pinnedPiece        // store the pinned piece location
		if pinnedPiece&ourPieces.Pawns != 0 { // it's a pawn; we might be able to push it
			if sameFile { // push the pawn
				var pawnTargets uint64 = 0
				pawnTargets |= (1 << uint8(int(pinnedPieceIdx)+8*pawnPushDirection)) & ^allPieces
				if pawnTargets != 0 { // single push worked; try double
					pawnTargets |= (1 << uint8(int(pinnedPieceIdx)+16*pawnPushDirection)) & ^allPieces & doublePushRank
				}
				pawnTargets &= allowDest // TODO this might be a promotion. Is that possible?
				genMovesFromTargets(moveList, Square(pinnedPieceIdx), pawnTargets)
			}
			continue
		}
		// If it's not a rook or queen, it can't move
		if pinnedPiece&ourPieces.Rooks == 0 && pinnedPiece&ourPieces.Queens == 0 {
			continue
		}
		// all ortho moves, as if it was not pinned
		pinnedPieceAllMoves := CalculateRookMoveBitboard(pinnedPieceIdx, allPieces) & (^(ourPieces.All))
		// actually available moves
		pinnedTargets := pinnedPieceAllMoves & (rookTargets | kingOrthoTargets | (uint64(1) << currRookIdx))
		pinnedTargets &= allowDest
		genMovesFromTargets(moveList, Square(pinnedPieceIdx), pinnedTargets)
	}

	// Calculate king moves as if it was a bishop.
	// "king targets" includes our own friendly pieces, for the purpose of identifying pins.
	kingDiagTargets := CalculateBishopMoveBitboard(ourKingIdx, allPieces)
	oppBishops := oppPieces.Bishops | oppPieces.Queens
	for oppBishops != 0 {
		currBishopIdx := uint8(bits.TrailingZeros64(oppBishops))
		oppBishops &= oppBishops - 1
		bishopTargets := CalculateBishopMoveBitboard(currBishopIdx, allPieces) & (^(oppPieces.All))
		pinnedPiece := bishopTargets & kingDiagTargets & ourPieces.All
		if pinnedPiece == 0 { // there is no pin
			continue
		}
		pinnedPieceIdx := uint8(bits.TrailingZeros64(pinnedPiece))
		bishopToPinnedSlope := (float32(pinnedPieceIdx)/8 - float32(currBishopIdx)/8) /
			(float32(pinnedPieceIdx%8) - float32(currBishopIdx%8))
		bishopToKingSlope := (float32(ourKingIdx)/8 - float32(currBishopIdx)/8) /
			(float32(ourKingIdx%8) - float32(currBishopIdx%8))
		if bishopToPinnedSlope != bishopToKingSlope { // just an intersection, not a pin
			continue
		}
		allPinnedPieces |= pinnedPiece // store pinned piece
		// if it's a pawn we might be able to capture with it
		// the capture square must also be in allowdest
		if (pinnedPiece & ourPieces.Pawns) != 0 {
			if (uint64(1) << currBishopIdx) & allowDest != 0 {
				// TODO - no branch
				if (b.Colortomove == White && (pinnedPieceIdx/8) + 1 == currBishopIdx/8) ||
					(b.Colortomove == Black && pinnedPieceIdx/8 == (currBishopIdx/8) + 1) {
					if ((uint64(1) << currBishopIdx) & ourPromotionRank) != 0 { // We get to promote!
						for i := Piece(Knight); i <= Queen; i++ {
							var move Move
							move.Setfrom(Square(pinnedPieceIdx)).Setto(Square(currBishopIdx)).Setpromote(i)
							*moveList = append(*moveList, move)
						}
					} else { // no promotion
						var move Move
						move.Setfrom(Square(pinnedPieceIdx)).Setto(Square(currBishopIdx))
						*moveList = append(*moveList, move)
					}
				}
			}
			continue
		}
		// If it's not a bishop or queen, it can't move
		if pinnedPiece&ourPieces.Bishops == 0 && pinnedPiece&ourPieces.Queens == 0 {
			continue
		}
		// all diag moves, as if it was not pinned
		pinnedPieceAllMoves := CalculateBishopMoveBitboard(pinnedPieceIdx, allPieces) & (^(ourPieces.All))
		// actually available moves
		pinnedTargets := pinnedPieceAllMoves & (bishopTargets | kingDiagTargets | (uint64(1) << currBishopIdx))
		pinnedTargets &= allowDest
		genMovesFromTargets(moveList, Square(pinnedPieceIdx), pinnedTargets)
	}
	return allPinnedPieces
}

// Generate moves involving advancing pawns.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) pawnPushes(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	targets, doubleTargets := b.pawnPushBitboards(nonpinned)

	ourCol := b.Colortomove
	oneRankBack := oneRankBacks[ourCol]
	
	targets, doubleTargets = targets&allowDest, doubleTargets&allowDest
	// push all pawns by one square
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1 // unset the lowest active bit
		var canPromote bool
		// TODO no branch
		if b.Colortomove == White {
			canPromote = target >= 56
		} else {
			canPromote = target <= 7
		}
		var move Move
		move.Setfrom(Square(target + oneRankBack)).Setto(Square(target))
		if canPromote {
			for i := Piece(Knight); i <= Queen; i++ {
				move.Setpromote(i)
				*moveList = append(*moveList, move)
			}
		} else {
			*moveList = append(*moveList, move)
		}
	}
	// push some pawns by two squares
	for doubleTargets != 0 {
		doubleTarget := bits.TrailingZeros64(doubleTargets)
		doubleTargets &= doubleTargets - 1 // unset the lowest active bit
		var move Move
		move.Setfrom(Square(doubleTarget + 2*oneRankBack)).Setto(Square(doubleTarget))
		*moveList = append(*moveList, move)
	}
}

// A helper function that produces bitboards of valid pawn push locations.
func (b *Board) pawnPushBitboards(nonpinned uint64) (targets uint64, doubleTargets uint64) {
	free := (^b.Bitboards[White].All) & (^b.Bitboards[Black].All)
	ourCol := b.Colortomove
	ourPawns := b.Bitboards[ourCol].Pawns
	// TODO no branch
	if b.Colortomove == White {
		movableWhitePawns := ourPawns & nonpinned
		targets = movableWhitePawns << 8 & free
		doubleTargets = targets << 8 & onlyRank[3] & free
	} else {
		movableBlackPawns := ourPawns & nonpinned
		targets = movableBlackPawns >> 8 & free
		doubleTargets = targets >> 8 & onlyRank[4] & free
	}
	return
}

// A function that computes available pawn captures.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) pawnCaptures(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	east, west := b.pawnCaptureBitboards(nonpinned)
	if b.enpassant > 0 { // always allow us to try en-passant captures
		allowDest = allowDest | (uint64(1) << b.enpassant)
	}
	east, west = east&allowDest, west&allowDest
	// TODO no branch
	dirbitboards := [2]uint64{east, west}
	if b.Colortomove == Black {
		dirbitboards[0], dirbitboards[1] = dirbitboards[1], dirbitboards[0]
	}
	for dir, board := range dirbitboards { // for east and west
		for board != 0 {
			target := bits.TrailingZeros64(board)
			board &= board - 1
			var move Move
			move.Setto(Square(target))
			canPromote := false
			// TODO no branch
			if b.Colortomove == White {
				move.Setfrom(Square(target - (9 - (dir * 2))))
				canPromote = target >= 56
			} else {
				move.Setfrom(Square(target + (9 - (dir * 2))))
				canPromote = target <= 7
			}
			if uint8(target) == b.enpassant && b.enpassant != 0 {
				// Apply, check actual legality, then unapply
				// Warning: not thread safe
				ourCol := b.Colortomove
				ourPieces := &b.Bitboards[ourCol]
				oppCol := oppColor(ourCol)
				oppPieces := &b.Bitboards[oppCol]
				enpassantEnemy := uint8(int(move.To()) + oneRankBacks[ourCol]) // Ugh

				ourPieces.Pawns &= ^(uint64(1) << move.From())
				ourPieces.All &= ^(uint64(1) << move.From())
				ourPieces.Pawns |= (uint64(1) << move.To())
				ourPieces.All |= (uint64(1) << move.To())
				oppPieces.Pawns &= ^(uint64(1) << enpassantEnemy)
				oppPieces.All &= ^(uint64(1) << enpassantEnemy)
				kingInCheck := b.OurKingInCheck()
				ourPieces.Pawns |= (uint64(1) << move.From())
				ourPieces.All |= (uint64(1) << move.From())
				ourPieces.Pawns &= ^(uint64(1) << move.To())
				ourPieces.All &= ^(uint64(1) << move.To())
				oppPieces.Pawns |= (uint64(1) << enpassantEnemy)
				oppPieces.All |= (uint64(1) << enpassantEnemy)
				if kingInCheck {
					continue
				}
			}
			if canPromote {
				for i := Piece(Knight); i <= Queen; i++ {
					move.Setpromote(i)
					*moveList = append(*moveList, move)
				}
				continue
			}
			*moveList = append(*moveList, move)
		}
	}
}

// A helper than generates bitboards for available pawn captures.
func (b *Board) pawnCaptureBitboards(nonpinned uint64) (east uint64, west uint64) {
	notHFile := uint64(0x7F7F7F7F7F7F7F7F)
	notAFile := uint64(0xFEFEFEFEFEFEFEFE)

	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]
	oppCol := oppColor(ourCol)
	oppPieces := &b.Bitboards[oppCol]
	
	targets := oppPieces.All
	// TODO(dylhunn): Always try the en passant capture and verify check status, regardless of
	//   valid square requirements
	if b.enpassant > 0 { // an en-passant target is active
		targets |= uint64(1) << b.enpassant
	}

	ourpawns := ourPieces.Pawns & nonpinned
	
	// TODO no branch
	if b.Colortomove == White {
		east = ourpawns << 9 & notAFile & targets
		west = ourpawns << 7 & notHFile & targets
	} else {
		east = ourpawns >> 7 & notAFile & targets
		west = ourpawns >> 9 & notHFile & targets
	}
	return
}

// Generate all knight moves.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) knightMoves(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]

	ourKnights := ourPieces.Knights & nonpinned
	noFriendlyPieces := ^ourPieces.All
	for ourKnights != 0 {
		currentKnight := bits.TrailingZeros64(ourKnights)
		ourKnights &= ourKnights - 1
		targets := knightMasks[currentKnight] & noFriendlyPieces & allowDest
		genMovesFromTargets(moveList, Square(currentKnight), targets)
	}
}

// Computes king moves excluding castling.
// TODO remove ptrToOurBitboards param
func (b *Board) kingPushes(moveList *[]Move, ptrToOurBitboards *Bitboards, allowDest uint64) {
	ourKingLocation := uint8(bits.TrailingZeros64(ptrToOurBitboards.Kings))
	noFriendlyPieces := ^(ptrToOurBitboards.All)

	// TODO(dylhunn): Modifying the board is NOT thread-safe.
	// We only do this to avoid the king danger problem, aka moving away from a
	// checking slider.
	oldKings := ptrToOurBitboards.Kings
	ptrToOurBitboards.Kings = 0
	ptrToOurBitboards.All &= ^(uint64(1) << ourKingLocation)
	targets := kingMasks[ourKingLocation] & noFriendlyPieces & allowDest
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1
		// TODO
		if b.UnderDirectAttack(b.Colortomove == White, uint8(target)) {
			continue
		}
		var move Move
		move.Setfrom(Square(ourKingLocation)).Setto(Square(target))
		*moveList = append(*moveList, move)
	}

	ptrToOurBitboards.Kings = oldKings
	ptrToOurBitboards.All |= (1 << ourKingLocation)
}

// Generate all available king moves.
// First, if castling is possible, verifies the checking prohibitions on castling.
// Then, outputs castling moves (if any), and king moves.
// Not thread-safe, since the king is removed from the board to compute
// king-danger squares.
func (b *Board) kingMoves(moveList *[]Move, allowDest uint64, includeCastling bool) {
	ourCol := b.Colortomove
	ptrToOurBitboards := &b.Bitboards[ourCol]
	
	if includeCastling {
		// castling
		ourKingLocation := uint8(bits.TrailingZeros64(ptrToOurBitboards.Kings))
		var canCastleQueenside, canCastleKingside bool
		allPieces := b.Bitboards[White].All | b.Bitboards[Black].All
		// TODO no branch
		if b.Colortomove == White {
			// To castle, we must have rights and a clear path
			kingsideClear := allPieces&((1<<5)|(1<<6)) == 0
			queensideClear := allPieces&((1<<3)|(1<<2)|(1<<1)) == 0
			// skip the king square, since this won't be called while in check
			canCastleQueenside = b.canCastle(White, Queenside) &&
				queensideClear && !b.anyUnderDirectAttack(true, 2, 3)
			canCastleKingside = b.canCastle(White, Kingside) &&
				kingsideClear && !b.anyUnderDirectAttack(true, 5, 6)
		} else {
			kingsideClear := allPieces&((1<<61)|(1<<62)) == 0
			queensideClear := allPieces&((1<<57)|(1<<58)|(1<<59)) == 0
			// skip the king square, since this won't be called while in check
			canCastleQueenside = b.canCastle(Black, Queenside) &&
				queensideClear && !b.anyUnderDirectAttack(false, 58, 59)
			canCastleKingside = b.canCastle(Black, Kingside) &&
				kingsideClear && !b.anyUnderDirectAttack(false, 61, 62)
		}
		if canCastleKingside {
			var move Move
			move.Setfrom(Square(ourKingLocation)).Setto(Square(ourKingLocation + 2))
			*moveList = append(*moveList, move)
		}
		if canCastleQueenside {
			var move Move
			move.Setfrom(Square(ourKingLocation)).Setto(Square(ourKingLocation - 2))
			*moveList = append(*moveList, move)
		}
	}

	// non-castling
	b.kingPushes(moveList, ptrToOurBitboards, allowDest)
}

// Generate all rook moves using magic bitboards.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) rookMoves(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]

	ourRooks := ourPieces.Rooks & nonpinned
	friendlyPieces := ourPieces.All

	allPieces := b.Bitboards[White].All | b.Bitboards[Black].All

	for ourRooks != 0 {
		currRook := uint8(bits.TrailingZeros64(ourRooks))
		ourRooks &= ourRooks - 1
		targets := CalculateRookMoveBitboard(currRook, allPieces) & (^friendlyPieces) & allowDest
		genMovesFromTargets(moveList, Square(currRook), targets)
	}
}

// Generate all bishop moves using magic bitboards.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) bishopMoves(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]

	ourBishops := ourPieces.Bishops & nonpinned
	friendlyPieces := ourPieces.All

	allPieces := b.Bitboards[White].All | b.Bitboards[Black].All
	
	for ourBishops != 0 {
		currBishop := uint8(bits.TrailingZeros64(ourBishops))
		ourBishops &= ourBishops - 1
		targets := CalculateBishopMoveBitboard(currBishop, allPieces) & (^friendlyPieces) & allowDest
		genMovesFromTargets(moveList, Square(currBishop), targets)
	}
}

// Generate all queen moves using magic bitboards.
// Only pieces marked nonpinned can be moved. Only squares in allowDest can be moved to.
func (b *Board) queenMoves(moveList *[]Move, nonpinned uint64, allowDest uint64) {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]

	ourQueens := ourPieces.Queens & nonpinned
	friendlyPieces := ourPieces.All

	allPieces := b.Bitboards[White].All | b.Bitboards[Black].All
	
	for ourQueens != 0 {
		currQueen := uint8(bits.TrailingZeros64(ourQueens))
		ourQueens &= ourQueens - 1
		// bishop motion
		diag_targets := CalculateBishopMoveBitboard(currQueen, allPieces) & (^friendlyPieces) & allowDest
		genMovesFromTargets(moveList, Square(currQueen), diag_targets)
		// rook motion
		ortho_targets := CalculateRookMoveBitboard(currQueen, allPieces) & (^friendlyPieces) & allowDest
		genMovesFromTargets(moveList, Square(currQueen), ortho_targets)
	}
}

// Helper: converts a targets bitboard into moves, and adds them to the moves list.
func genMovesFromTargets(moveList *[]Move, origin Square, targets uint64) {
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1
		var move Move
		move.Setfrom(origin).Setto(Square(target))
		*moveList = append(*moveList, move)
	}
}

// Variadic function that returns whether any of the specified squares is being attacked
// by the opponent. Potentially expensive.
func (b *Board) anyUnderDirectAttack(byBlack bool, squares ...uint8) bool {
	for _, v := range squares {
		if b.UnderDirectAttack(byBlack, v) {
			return true
		}
	}
	return false
}

func (b *Board) OurKingInCheck() bool {
	ourCol := b.Colortomove
	ourPieces := &b.Bitboards[ourCol]

	// assumes only one king
	origin := uint8(bits.TrailingZeros64(ourPieces.Kings))

	// TODO
	count, _ := b.countAttacks(b.Colortomove == White, origin, 1)
	return count >= 1
}

// Determine if a square is under attack. Potentially expensive.
func (b *Board) UnderDirectAttack(byBlack bool, origin uint8) bool {
	count, _ := b.countAttacks(byBlack, origin, 1)
	return count >= 1
}

// Compute whether an individual square is under direct attack. Potentially expensive.
// Can be asked to abort early, when a certain number of attacks are found.
// The found number might exceed the abortion threshold, since attacks are grouped.
// Also returns the mask of attackers.
func (b *Board) countAttacks(byBlack bool, origin uint8, abortEarly int) (int, uint64) {
	numAttacks := 0
	var blockerDestinations uint64 = 0

	//ourCol := b.Colortomove
	//ourPieces := &b.Bitboards[ourCol]
	//oppCol := oppColor(ourCol)
	//oppPieces := &b.Bitboards[oppCol]
	var oppPieces *Bitboards
	if byBlack {
		oppPieces = &(b.Bitboards[Black])
	} else {
		oppPieces = &(b.Bitboards[White])
	}
	
	allPieces := b.Bitboards[White].All | b.Bitboards[Black].All

	// find attacking knights
	knight_attackers := knightMasks[origin] & oppPieces.Knights
	numAttacks += bits.OnesCount64(knight_attackers)
	blockerDestinations |= knight_attackers
	if numAttacks >= abortEarly {
		return numAttacks, blockerDestinations
	}
	// find attacking bishops and queens
	diag_candidates := magicBishopBlockerMasks[origin] & allPieces
	diag_dbindex := (diag_candidates * magicNumberBishop[origin]) >> magicBishopShifts[origin]
	origin_diag_rays := magicMovesBishop[origin][diag_dbindex]
	diag_attackers := origin_diag_rays & (oppPieces.Bishops | oppPieces.Queens)
	numAttacks += bits.OnesCount64(diag_attackers)
	blockerDestinations |= diag_attackers
	if numAttacks >= abortEarly {
		return numAttacks, blockerDestinations
	}
	// If we found diagonal attackers, add interposed squares to the blocker mask.
	for diag_attackers != 0 {
		curr_attacker := uint8(bits.TrailingZeros64(diag_attackers))
		diag_attackers &= diag_attackers - 1
		diag_attacks := CalculateBishopMoveBitboard(curr_attacker, allPieces)
		attackRay := diag_attacks & origin_diag_rays
		blockerDestinations |= attackRay
	}

	// find attacking rooks and queens
	ortho_candidates := magicRookBlockerMasks[origin] & allPieces
	ortho_dbindex := (ortho_candidates * magicNumberRook[origin]) >> magicRookShifts[origin]
	origin_ortho_rays := magicMovesRook[origin][ortho_dbindex]
	ortho_attackers := origin_ortho_rays & (oppPieces.Rooks | oppPieces.Queens)
	numAttacks += bits.OnesCount64(ortho_attackers)
	blockerDestinations |= ortho_attackers
	if numAttacks >= abortEarly {
		return numAttacks, blockerDestinations
	}
	// If we found orthogonal attackers, add interposed squares to the blocker mask.
	for ortho_attackers != 0 {
		curr_attacker := uint8(bits.TrailingZeros64(ortho_attackers))
		ortho_attackers &= ortho_attackers - 1
		ortho_attacks := CalculateRookMoveBitboard(curr_attacker, allPieces)
		attackRay := ortho_attacks & origin_ortho_rays
		blockerDestinations |= attackRay
	}
	// find attacking kings
	// TODO(dylhunn): What if the opponent king can't actually move to the origin square?
	king_attackers := kingMasks[origin] & oppPieces.Kings
	numAttacks += bits.OnesCount64(king_attackers)
	blockerDestinations |= king_attackers
	if numAttacks >= abortEarly {
		return numAttacks, blockerDestinations
	}
	// find attacking pawns
	var pawn_attackers_mask uint64 = 0
	if byBlack {
		pawn_attackers_mask = (1 << (origin + 7)) & ^(onlyFile[7])
		pawn_attackers_mask |= (1 << (origin + 9)) & ^(onlyFile[0])
	} else {
		if origin-7 >= 0 {
			pawn_attackers_mask = (1 << (origin - 7)) & ^(onlyFile[0])
		}
		if origin-9 >= 0 {
			pawn_attackers_mask |= (1 << (origin - 9)) & ^(onlyFile[7])
		}
	}
	pawn_attackers_mask &= oppPieces.Pawns
	numAttacks += bits.OnesCount64(pawn_attackers_mask)
	blockerDestinations |= pawn_attackers_mask
	if numAttacks >= abortEarly {
		return numAttacks, blockerDestinations
	}
	return numAttacks, blockerDestinations
}

// Calculates the attack bitboard for a rook. This might include targeted squares
// that are actually friendly pieces, so the proper usage is:
// rookTargets := CalculateRookMoveBitboard(myRookLoc, allPieces) & (^myPieces)
// Externally useful for evaluation functions.
func CalculateRookMoveBitboard(currRook uint8, allPieces uint64) uint64 {
	blockers := magicRookBlockerMasks[currRook] & allPieces
	dbindex := (blockers * magicNumberRook[currRook]) >> magicRookShifts[currRook]
	targets := magicMovesRook[currRook][dbindex]
	return targets
}

// Calculates the attack bitboard for a bishop. This might include targeted squares
// that are actually friendly pieces, so the proper usage is:
// bishopTargets := CalculateBishopMoveBitboard(myBishopLoc, allPieces) & (^myPieces)
// Externally useful for evaluation functions.
func CalculateBishopMoveBitboard(currBishop uint8, allPieces uint64) uint64 {
	blockers := magicBishopBlockerMasks[currBishop] & allPieces
	dbindex := (blockers * magicNumberBishop[currBishop]) >> magicBishopShifts[currBishop]
	targets := magicMovesBishop[currBishop][dbindex]
	return targets
}
