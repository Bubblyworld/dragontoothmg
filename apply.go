package dragontoothmg

import (
	//"fmt"
)

// Encapsulation of move take-back.
// Instead of undoing all specific steps we save minimal state from before the move.
type BoardSaveT struct {
	Enpassant uint8
	Halfmoveclock uint8
	Castlerights [NColors]CastleRightsFlagsT
	
	FromLoc uint8
	ToLoc uint8
	CaptureLoc uint8
	FromPiece Piece
	ToPiece Piece // Different from fromPiece only for promotions
	CapturePiece Piece // Nothing if no capture
	
	FromBb uint64 // Piece bitboard of the moved piece
	ToBb uint64 // Different from fromBb only for promotions
	CaptureBb uint64 // (opposition) bitboard for capture piece

	// Rook state from before the move in case it was a castling move
	// For a non-castling move the rook from- and to loc's are 0
	OurRookFrom uint8
	OurRookTo uint8
	OurRookBb uint64

	OurAllBb uint64
	OppAllBb uint64
	
	Hash uint64
}

// Take back move - still likely less efficient than a bulk copy of the whole Board structure :P
func (b *Board) Restore(bs *BoardSaveT) {
	oppCol := b.Colortomove
	ourCol := oppColor(oppCol)

	b.Colortomove = ourCol

	b.enpassant = bs.Enpassant
	
	b.Halfmoveclock = bs.Halfmoveclock
	b.Fullmoveno -= uint16(ourCol)

	b.castlerights = bs.Castlerights

	// Ordering here is important - undo before undoing capture
	b.Bbs[ourCol][bs.ToPiece] = bs.ToBb
	b.pieces[bs.ToLoc] = Nothing

	b.Bbs[oppCol][bs.CapturePiece] = bs.CaptureBb
	b.pieces[bs.CaptureLoc] = bs.CapturePiece

	b.Bbs[ourCol][bs.FromPiece] = bs.FromBb
	b.pieces[bs.FromLoc] = bs.FromPiece

	b.Bbs[ourCol][Rook] = bs.OurRookBb
	// Unmove rook castling move - must be a nop for non-castling
	maybeRook := b.pieces[bs.OurRookTo]
	b.pieces[bs.OurRookTo] = Nothing
	b.pieces[bs.OurRookFrom] = maybeRook // this will write back the original square 0 piece if this is not actually a castling move

	b.Bbs[ourCol][All] = bs.OurAllBb
	b.Bbs[oppCol][All] = bs.OppAllBb

	b.hash = bs.Hash
}

// Add this to the e.p. square to find the captured pawn for each colour
var epDeltas = [NColors]int8 {-8, 8}

var startingRankBbs = [NColors]uint64 {onlyRank[0], onlyRank[7]}

var piecesPawnZobristIndexes = [NColors]int {0, 6}

// Applies a move to the board, and fills in a restore structure for subsequent move take-back.
// This function assumes that the given move is valid (i.e., is in the set of moves found by GenerateLegalMoves()).
// If the move is not valid, this function has undefined behavior.
func (b *Board) MakeMove(m Move, bs *BoardSaveT) {
	if m.IsSimple() {
		b.MakeSimpleMove(m, bs)
	} else {
		b.MakeSpecialMove(m, bs)
	}
}

func (b *Board) MakeSimpleMove(m Move, bs *BoardSaveT) {

	bs.Enpassant = b.enpassant
	bs.Halfmoveclock = b.Halfmoveclock
	bs.Castlerights = b.castlerights
	
	ourCol := b.Colortomove
	oppCol := oppColor(ourCol)

	bs.OurRookFrom = 0
	bs.OurRookTo = 0
	bs.OurRookBb = b.Bbs[ourCol][Rook]
	bs.OurAllBb = b.Bbs[ourCol][All]
	bs.OppAllBb = b.Bbs[oppCol][All]
	bs.Hash = b.hash
	
	// Configure data about which pieces move
	ourBitboardPtr, oppBitboardPtr := &b.Bbs[ourCol], &b.Bbs[oppCol]
	epDelta := epDeltas[ourCol] // add this to the e.p. square to find the captured pawn
	ourStartingRankBb, oppStartingRankBb := startingRankBbs[ourCol], startingRankBbs[oppCol] // the starting rank of each side
	// the constant that represents the index into pieceSquareZobristC for the pawn of our color
	ourPiecesPawnZobristIndex, oppPiecesPawnZobristIndex := piecesPawnZobristIndexes[ourCol], piecesPawnZobristIndexes[oppCol]

	// increment after black's move
	b.Fullmoveno += uint16(ourCol) 

	fromLoc := m.From()
	bs.FromLoc = fromLoc
	fromBitboard := (uint64(1) << fromLoc)
	toLoc := m.To()
	bs.ToLoc = toLoc
	bs.CaptureLoc = toLoc
	toBitboard := (uint64(1) << toLoc)
	pieceType, pieceTypeBitboard := determinePieceType(b, ourBitboardPtr, fromBitboard, m.From())

	bs.FromPiece = pieceType
	bs.FromBb = b.Bbs[ourCol][pieceType]
	bs.ToPiece = pieceType
	bs.ToBb = bs.FromBb
	bs.CapturePiece = Nothing
	bs.CaptureBb = 0

	// castleStatus := 0
	// var oldRookLoc, newRookLoc uint8

	// If it is any kind of capture or pawn move, reset halfmove clock.
	// TODO IsCapture??? - should be cheaper to calculate later...
	if IsCapture(m, b) || pieceType == Pawn { 
		b.Halfmoveclock = 0 // reset halfmove clock
	} else {
		b.Halfmoveclock++
	}

	// King moves strip castling rights
	if pieceType == King {
		// // TODO(dylhunn): do this without a branch
		// if m.To()-m.From() == 2 { // castle short
		// 	castleStatus = 1
		// 	oldRookLoc = m.To() + 1
		// 	newRookLoc = m.To() - 1
		// } else if int(m.To())-int(m.From()) == -2 { // castle long
		// 	castleStatus = -1
		// 	oldRookLoc = m.To() - 2
		// 	newRookLoc = m.To() + 1
		// }
		// King moves always strip castling rights
		if b.weCanCastle(Kingside) {
			b.flipOurCastleRights(Kingside)
		}
		if b.weCanCastle(Queenside) {
			b.flipOurCastleRights(Queenside)
		}
	}

	// Rook moves strip castling rights
	if pieceType == Rook {
		if b.weCanCastle(Kingside) && (fromBitboard&onlyFile[7] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // king's rook
			b.flipOurCastleRights(Kingside)
		} else if b.weCanCastle(Queenside) && (fromBitboard&onlyFile[0] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // queen's rook
			b.flipOurCastleRights(Queenside)
		}
	}

	// Apply the castling rook movement
	// if castleStatus != 0 {
	// 	bs.OurRookFrom = oldRookLoc
	// 	bs.OurRookTo = newRookLoc
		
	// 	b.movePiece(Rook, Rook, oldRookLoc, newRookLoc, &ourBitboardPtr[Rook], &ourBitboardPtr[Rook], &ourBitboardPtr[All]) // ??? Flumoxed
	// 	// Update rook location in hash
	// 	// (Rook - 1) assumes that "Nothing" precedes "Rook" in the Piece constants list
	// 	b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(int(Rook)-1)][oldRookLoc]
	// 	b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(int(Rook)-1)][newRookLoc]
	// }

	// Is this an e.p. capture? Strip the opponent pawn and reset the e.p. square
	oldEpCaptureSquare := b.enpassant
	// if pieceType == Pawn && m.To() == oldEpCaptureSquare && oldEpCaptureSquare != 0 {
	// 	fmt.Println("Boooooooooooooooooooooooooo!!! en-passant in simple move !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!1")
	// 	epOpponentPawnLocation := uint8(int8(oldEpCaptureSquare) + epDelta)

	// 	bs.CapturePiece = Pawn
	// 	bs.CaptureLoc = epOpponentPawnLocation
	// 	bs.CaptureBb = b.Bbs[oppCol][Pawn]

	// 	b.removePiece(Pawn, epOpponentPawnLocation, &oppBitboardPtr[Pawn], &oppBitboardPtr[All])
	// 	// Remove the opponent pawn from the board hash.
	// 	b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex][epOpponentPawnLocation]
	// }
	// Update the en passant square
	if pieceType == Pawn && (int8(m.To())+2*epDelta == int8(m.From())) { // pawn double push
		b.enpassant = uint8(int8(m.To()) + epDelta)
	} else {
		b.enpassant = 0
	}

	// Is this a promotion?
	var destTypeBitboard *uint64 = pieceTypeBitboard
	var promotedToPieceType Piece = pieceType // if not promoted, same as pieceType
	// switch m.Promote() {
	// case Queen:
	// 	destTypeBitboard = &(ourBitboardPtr[Queen])
	// 	promotedToPieceType = Queen
	// case Knight:
	// 	destTypeBitboard = &(ourBitboardPtr[Knight])
	// 	promotedToPieceType = Knight
	// case Rook:
	// 	destTypeBitboard = &(ourBitboardPtr[Rook])
	// 	promotedToPieceType = Rook
	// case Bishop:
	// 	destTypeBitboard = &(ourBitboardPtr[Bishop])
	// 	promotedToPieceType = Bishop
	// default:
	// 	destTypeBitboard = pieceTypeBitboard
	// 	promotedToPieceType = pieceType
	// }

	//moveApplication.ToPieceType = promotedToPieceType
	bs.ToPiece = promotedToPieceType
	bs.ToBb = b.Bbs[ourCol][promotedToPieceType]

	// Apply the move - remove the captured piece first so that we don't overwrite the moved piece
	capturedPieceType, capturedBitboard := determinePieceType(b, oppBitboardPtr, toBitboard, m.To())
	if capturedPieceType != Nothing {   // This does not account for e.p. captures
		bs.CapturePiece = capturedPieceType
		bs.CaptureBb = b.Bbs[oppCol][capturedPieceType]
		
		b.removePiece(capturedPieceType, m.To(), capturedBitboard, &oppBitboardPtr[All])
		b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex+(int(capturedPieceType)-1)][m.To()] // remove the captured piece from the hash - TODO (RPJ) wrong capture location for en-passant?
	}
	b.movePiece(pieceType, promotedToPieceType, m.From(), m.To(), pieceTypeBitboard, destTypeBitboard, &ourBitboardPtr[All])
	b.hash ^= pieceSquareZobristC[(int(pieceType)-1)+ourPiecesPawnZobristIndex][m.From()]         // remove piece at "from"
	b.hash ^= pieceSquareZobristC[(int(promotedToPieceType)-1)+ourPiecesPawnZobristIndex][m.To()] // add piece at "to"

	// If a rook was captured, it strips castling rights
	if capturedPieceType == Rook {
		if m.To()%8 == 7 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastle(Kingside) { // captured king rook
			b.flipOppCastleRights(Kingside)
		} else if m.To()%8 == 0 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastle(Queenside) { // queen rooks
			b.flipOppCastleRights(Queenside)
		}
	}
	// flip the side to move in the hash
	b.hash ^= whiteToMoveZobristC
	b.Colortomove = oppColor(b.Colortomove)

	// remove the old en passant square from the hash, and add the new one
	b.hash ^= uint64(oldEpCaptureSquare)
	b.hash ^= uint64(b.enpassant)
}

// Applies a move to the board, and fills in a restore structure for subsequent move take-back.
// This function assumes that the given move is valid (i.e., is in the set of moves found by GenerateLegalMoves()).
// If the move is not valid, this function has undefined behavior.
func (b *Board) MakeSpecialMove(m Move, bs *BoardSaveT) {

	bs.Enpassant = b.enpassant
	bs.Halfmoveclock = b.Halfmoveclock
	bs.Castlerights = b.castlerights
	
	ourCol := b.Colortomove
	oppCol := oppColor(ourCol)

	bs.OurRookFrom = 0
	bs.OurRookTo = 0
	bs.OurRookBb = b.Bbs[ourCol][Rook]
	bs.OurAllBb = b.Bbs[ourCol][All]
	bs.OppAllBb = b.Bbs[oppCol][All]
	bs.Hash = b.hash
	
	// Configure data about which pieces move
	ourBitboardPtr, oppBitboardPtr := &b.Bbs[ourCol], &b.Bbs[oppCol]
	epDelta := epDeltas[ourCol] // add this to the e.p. square to find the captured pawn
	ourStartingRankBb, oppStartingRankBb := startingRankBbs[ourCol], startingRankBbs[oppCol] // the starting rank of each side
	// the constant that represents the index into pieceSquareZobristC for the pawn of our color
	ourPiecesPawnZobristIndex, oppPiecesPawnZobristIndex := piecesPawnZobristIndexes[ourCol], piecesPawnZobristIndexes[oppCol]

	// increment after black's move
	b.Fullmoveno += uint16(ourCol) 

	fromLoc := m.From()
	bs.FromLoc = fromLoc
	fromBitboard := (uint64(1) << fromLoc)
	toLoc := m.To()
	bs.ToLoc = toLoc
	bs.CaptureLoc = toLoc
	toBitboard := (uint64(1) << toLoc)
	pieceType, pieceTypeBitboard := determinePieceType(b, ourBitboardPtr, fromBitboard, m.From())

	bs.FromPiece = pieceType
	bs.FromBb = b.Bbs[ourCol][pieceType]
	bs.ToPiece = pieceType
	bs.ToBb = bs.FromBb
	bs.CapturePiece = Nothing
	bs.CaptureBb = 0

	castleStatus := 0
	var oldRookLoc, newRookLoc uint8

	// If it is any kind of capture or pawn move, reset halfmove clock.
	// TODO IsCapture??? - should be cheaper to calculate later...
	if IsCapture(m, b) || pieceType == Pawn { 
		b.Halfmoveclock = 0 // reset halfmove clock
	} else {
		b.Halfmoveclock++
	}

	// King moves strip castling rights
	if pieceType == King {
		// TODO(dylhunn): do this without a branch
		if m.To()-m.From() == 2 { // castle short
			castleStatus = 1
			oldRookLoc = m.To() + 1
			newRookLoc = m.To() - 1
		} else if int(m.To())-int(m.From()) == -2 { // castle long
			castleStatus = -1
			oldRookLoc = m.To() - 2
			newRookLoc = m.To() + 1
		}
		// King moves always strip castling rights
		if b.weCanCastle(Kingside) {
			b.flipOurCastleRights(Kingside)
		}
		if b.weCanCastle(Queenside) {
			b.flipOurCastleRights(Queenside)
		}
	}

	// Rook moves strip castling rights
	if pieceType == Rook {
		if b.weCanCastle(Kingside) && (fromBitboard&onlyFile[7] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // king's rook
			b.flipOurCastleRights(Kingside)
		} else if b.weCanCastle(Queenside) && (fromBitboard&onlyFile[0] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // queen's rook
			b.flipOurCastleRights(Queenside)
		}
	}

	// Apply the castling rook movement
	if castleStatus != 0 {
		bs.OurRookFrom = oldRookLoc
		bs.OurRookTo = newRookLoc
		
		b.movePiece(Rook, Rook, oldRookLoc, newRookLoc, &ourBitboardPtr[Rook], &ourBitboardPtr[Rook], &ourBitboardPtr[All]) // ??? Flumoxed
		// Update rook location in hash
		// (Rook - 1) assumes that "Nothing" precedes "Rook" in the Piece constants list
		b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(int(Rook)-1)][oldRookLoc]
		b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(int(Rook)-1)][newRookLoc]
	}

	// Is this an e.p. capture? Strip the opponent pawn and reset the e.p. square
	oldEpCaptureSquare := b.enpassant
	if pieceType == Pawn && m.To() == oldEpCaptureSquare && oldEpCaptureSquare != 0 {
		epOpponentPawnLocation := uint8(int8(oldEpCaptureSquare) + epDelta)

		bs.CapturePiece = Pawn
		bs.CaptureLoc = epOpponentPawnLocation
		bs.CaptureBb = b.Bbs[oppCol][Pawn]

		b.removePiece(Pawn, epOpponentPawnLocation, &oppBitboardPtr[Pawn], &oppBitboardPtr[All])
		// Remove the opponent pawn from the board hash.
		b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex][epOpponentPawnLocation]
	}
	// Update the en passant square
	if pieceType == Pawn && (int8(m.To())+2*epDelta == int8(m.From())) { // pawn double push
		b.enpassant = uint8(int8(m.To()) + epDelta)
	} else {
		b.enpassant = 0
	}

	// Is this a promotion?
	var destTypeBitboard *uint64
	var promotedToPieceType Piece // if not promoted, same as pieceType
	switch m.Promote() {
	case Queen:
		destTypeBitboard = &(ourBitboardPtr[Queen])
		promotedToPieceType = Queen
	case Knight:
		destTypeBitboard = &(ourBitboardPtr[Knight])
		promotedToPieceType = Knight
	case Rook:
		destTypeBitboard = &(ourBitboardPtr[Rook])
		promotedToPieceType = Rook
	case Bishop:
		destTypeBitboard = &(ourBitboardPtr[Bishop])
		promotedToPieceType = Bishop
	default:
		destTypeBitboard = pieceTypeBitboard
		promotedToPieceType = pieceType
	}

	//moveApplication.ToPieceType = promotedToPieceType
	bs.ToPiece = promotedToPieceType
	bs.ToBb = b.Bbs[ourCol][promotedToPieceType]

	// Apply the move - remove the captured piece first so that we don't overwrite the moved piece
	capturedPieceType, capturedBitboard := determinePieceType(b, oppBitboardPtr, toBitboard, m.To())
	if capturedPieceType != Nothing {   // This does not account for e.p. captures
		bs.CapturePiece = capturedPieceType
		bs.CaptureBb = b.Bbs[oppCol][capturedPieceType]
		
		b.removePiece(capturedPieceType, m.To(), capturedBitboard, &oppBitboardPtr[All])
		b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex+(int(capturedPieceType)-1)][m.To()] // remove the captured piece from the hash - TODO (RPJ) wrong capture location for en-passant?
	}
	b.movePiece(pieceType, promotedToPieceType, m.From(), m.To(), pieceTypeBitboard, destTypeBitboard, &ourBitboardPtr[All])
	b.hash ^= pieceSquareZobristC[(int(pieceType)-1)+ourPiecesPawnZobristIndex][m.From()]         // remove piece at "from"
	b.hash ^= pieceSquareZobristC[(int(promotedToPieceType)-1)+ourPiecesPawnZobristIndex][m.To()] // add piece at "to"

	// If a rook was captured, it strips castling rights
	if capturedPieceType == Rook {
		if m.To()%8 == 7 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastle(Kingside) { // captured king rook
			b.flipOppCastleRights(Kingside)
		} else if m.To()%8 == 0 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastle(Queenside) { // queen rooks
			b.flipOppCastleRights(Queenside)
		}
	}
	// flip the side to move in the hash
	b.hash ^= whiteToMoveZobristC
	b.Colortomove = oppColor(b.Colortomove)

	// remove the old en passant square from the hash, and add the new one
	b.hash ^= uint64(oldEpCaptureSquare)
	b.hash ^= uint64(b.enpassant)
}

// Applies a null move to the board, and returns a function that can be used to unapply it.
// A null move is just that - the current player skips his move.
// Used for Null Move Heuristic in the search engine.
func (b *Board) ApplyNullMove() func() {
	return b.ApplyNullMove2().Unapply
}

func (b *Board) ApplyNullMove2() MoveApplication {
	var moveInfo MoveApplication
	
	// TODO - half-move clock?

	// Clear the en-passant square
	oldEpCaptureSquare := b.enpassant
	b.enpassant = 0

	// remove the old en passant square from the hash, and add the new one
	b.hash ^= uint64(oldEpCaptureSquare)

	// flip the side to move in the hash
	b.hash ^= whiteToMoveZobristC
	// b.Wtomove = !b.Wtomove
	b.Colortomove = oppColor(b.Colortomove)

	// Generate the unapply function (closure)
	moveInfo.Unapply = func() {
		// Flip the player to move
		b.hash ^= whiteToMoveZobristC
		// b.Wtomove = !b.Wtomove
		b.Colortomove = oppColor(b.Colortomove)

		// Unapply en-passant square change
		b.hash ^= uint64(oldEpCaptureSquare) // restore the old one to the hash
		b.enpassant = oldEpCaptureSquare
	}

	
	return moveInfo
}

func determinePieceType(b *Board, bb *Bitboards, squareMask uint64, pos uint8) (Piece, *uint64) {
	piece := b.PieceAt(pos)

	return piece, bb.pieceBitboard(piece)
}

// Legacy
// Applies a move to the board, and returns a function that can be used to unapply it.
// This function assumes that the given move is valid (i.e., is in the set of moves found by GenerateLegalMoves()).
// If the move is not valid, this function has undefined behavior.
func (b *Board) Apply(m Move) func() {
	var bs BoardSaveT
	b.MakeMove(m, &bs)

	return func() { b.Restore(&bs) }
}
