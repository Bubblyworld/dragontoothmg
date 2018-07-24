package dragontoothmg

import (
	//"fmt"
)

// Applies a move to the board, and returns a function that can be used to unapply it.
// This function assumes that the given move is valid (i.e., is in the set of moves found by GenerateLegalMoves()).
// If the move is not valid, this function has undefined behavior.
func (b *Board) Apply(m Move) func() {
	return b.Apply2(m).Unapply
}

// Add this to the e.p. square to find the captured pawn for each colour
var epDeltas = [NColors]int8 {-8, 8}

var startingRankBbs = [NColors]uint64 {onlyRank[0], onlyRank[7]}

var piecesPawnZobristIndexes = [NColors]int {0, 6}

// Applies a move to the board, and returns move application information and a function that can be used to unapply it.
// This function assumes that the given move is valid (i.e., is in the set of moves found by GenerateLegalMoves()).
// If the move is not valid, this function has undefined behavior.
func (b *Board) Apply2(m Move) *MoveApplication {
	var moveApplication MoveApplication
	moveApplication.Move = m

	ourCol := b.Colortomove
	oppCol := oppColor(ourCol)
	
	// Configure data about which pieces move
	ourBitboardPtr, oppBitboardPtr := &b.Bitboards[ourCol], &b.Bitboards[oppCol]
	epDelta := epDeltas[ourCol] // add this to the e.p. square to find the captured pawn
	ourStartingRankBb, oppStartingRankBb := startingRankBbs[ourCol], startingRankBbs[oppCol] // the starting rank of each side
	// the constant that represents the index into pieceSquareZobristC for the pawn of our color
	ourPiecesPawnZobristIndex, oppPiecesPawnZobristIndex := piecesPawnZobristIndexes[ourCol], piecesPawnZobristIndexes[oppCol]

	if b.Colortomove == Black {
		b.Fullmoveno++ // increment after black's move
	}

	fromBitboard := (uint64(1) << m.From())
	toBitboard := (uint64(1) << m.To())
	pieceType, pieceTypeBitboard := determinePieceType(b, ourBitboardPtr, fromBitboard, m.From())

	moveApplication.FromPieceType = pieceType
	moveApplication.CapturedPieceType = Nothing
	moveApplication.IsCastling = false
	
	castleStatus := 0
	var oldRookLoc, newRookLoc uint8
	var flippedKsCastle, flippedQsCastle, flippedOppKsCastle, flippedOppQsCastle bool

	// If it is any kind of capture or pawn move, reset halfmove clock.
	resetHalfmoveClockFrom := -1
	if IsCapture(m, b) || pieceType == Pawn { 
		resetHalfmoveClockFrom = int(b.Halfmoveclock)
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
		if b.canCastleKingside() {
			b.flipKingsideCastle()
			flippedKsCastle = true
		}
		if b.canCastleQueenside() {
			b.flipQueensideCastle()
			flippedQsCastle = true
		}
	}

	// Rook moves strip castling rights
	if pieceType == Rook {
		if b.canCastleKingside() && (fromBitboard&onlyFile[7] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // king's rook
			flippedKsCastle = true
			b.flipKingsideCastle()
		} else if b.canCastleQueenside() && (fromBitboard&onlyFile[0] != 0) &&
			fromBitboard&ourStartingRankBb != 0 { // queen's rook
			flippedQsCastle = true
			b.flipQueensideCastle()
		}
	}

	// Apply the castling rook movement
	if castleStatus != 0 {
		b.movePiece(Rook, Rook, oldRookLoc, newRookLoc, &ourBitboardPtr.Rooks, &ourBitboardPtr.Rooks, &ourBitboardPtr.All) // ??? Flumoxed
		// Update rook location in hash
		// (Rook - 1) assumes that "Nothing" precedes "Rook" in the Piece constants list
		b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(Rook-1)][oldRookLoc]
		b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(Rook-1)][newRookLoc]

		moveApplication.IsCastling = true
		moveApplication.RookCastleFrom = oldRookLoc
		moveApplication.RookCastleTo = newRookLoc
	}

	// Is this an e.p. capture? Strip the opponent pawn and reset the e.p. square
	oldEpCaptureSquare := b.enpassant
	var actuallyPerformedEpCapture bool = false
	if pieceType == Pawn && m.To() == oldEpCaptureSquare && oldEpCaptureSquare != 0 {
		actuallyPerformedEpCapture = true
		epOpponentPawnLocation := uint8(int8(oldEpCaptureSquare) + epDelta)
		b.removePiece(Pawn, epOpponentPawnLocation, &oppBitboardPtr.Pawns, &oppBitboardPtr.All)
		// Remove the opponent pawn from the board hash.
		b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex][epOpponentPawnLocation]
		
		moveApplication.CapturedPieceType = Pawn
		moveApplication.CaptureLocation = epOpponentPawnLocation
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
		destTypeBitboard = &(ourBitboardPtr.Queens)
		promotedToPieceType = Queen
	case Knight:
		destTypeBitboard = &(ourBitboardPtr.Knights)
		promotedToPieceType = Knight
	case Rook:
		destTypeBitboard = &(ourBitboardPtr.Rooks)
		promotedToPieceType = Rook
	case Bishop:
		destTypeBitboard = &(ourBitboardPtr.Bishops)
		promotedToPieceType = Bishop
	default:
		destTypeBitboard = pieceTypeBitboard
		promotedToPieceType = pieceType
	}

	moveApplication.ToPieceType = promotedToPieceType

	// Apply the move - remove the captured piece first so that we don't overwrite the moved piece
	capturedPieceType, capturedBitboard := determinePieceType(b, oppBitboardPtr, toBitboard, m.To())
	if capturedPieceType != Nothing {   // This does not account for e.p. captures
		b.removePiece(capturedPieceType, m.To(), capturedBitboard, &oppBitboardPtr.All)
		b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex+(int(capturedPieceType)-1)][m.To()] // remove the captured piece from the hash - TODO (RPJ) wrong capture location for en-passant?
		
		moveApplication.CapturedPieceType = capturedPieceType
		moveApplication.CaptureLocation = m.To()
	}
	b.movePiece(pieceType, promotedToPieceType, m.From(), m.To(), pieceTypeBitboard, destTypeBitboard, &ourBitboardPtr.All)
	b.hash ^= pieceSquareZobristC[(int(pieceType)-1)+ourPiecesPawnZobristIndex][m.From()]         // remove piece at "from"
	b.hash ^= pieceSquareZobristC[(int(promotedToPieceType)-1)+ourPiecesPawnZobristIndex][m.To()] // add piece at "to"

	// If a rook was captured, it strips castling rights
	if capturedPieceType == Rook {
		if m.To()%8 == 7 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastleKingside() { // captured king rook
			b.flipOppKingsideCastle()
			flippedOppKsCastle = true
		} else if m.To()%8 == 0 && toBitboard&oppStartingRankBb != 0 && b.oppCanCastleQueenside() { // queen rooks
			b.flipOppQueensideCastle()
			flippedOppQsCastle = true
		}
	}
	// flip the side to move in the hash
	b.hash ^= whiteToMoveZobristC
	//b.Wtomove = !b.Wtomove
	b.Colortomove = oppColor(b.Colortomove)

	// remove the old en passant square from the hash, and add the new one
	b.hash ^= uint64(oldEpCaptureSquare)
	b.hash ^= uint64(b.enpassant)

	// Generate the unapply function (closure)
	moveApplication.Unapply = func() {
		// Flip the player to move
		b.hash ^= whiteToMoveZobristC
		//b.Wtomove = !b.Wtomove
		b.Colortomove = oppColor(b.Colortomove)

		// Restore the halfmove clock
		if resetHalfmoveClockFrom == -1 {
			b.Halfmoveclock--
		} else {
			b.Halfmoveclock = uint8(resetHalfmoveClockFrom)
		}

		// Unapply move - reverse of original move
		b.movePiece(promotedToPieceType, pieceType, m.To(), m.From(), destTypeBitboard, pieceTypeBitboard, &ourBitboardPtr.All)
		b.hash ^= pieceSquareZobristC[(int(promotedToPieceType)-1)+ourPiecesPawnZobristIndex][m.To()] // remove the piece at "to"
		b.hash ^= pieceSquareZobristC[(int(pieceType)-1)+ourPiecesPawnZobristIndex][m.From()]         // add the piece at "from"

		// Restore captured piece (excluding e.p.)
		if capturedPieceType != Nothing { // doesn't consider e.p. captures
			b.addPiece(capturedPieceType, m.To(), capturedBitboard, &oppBitboardPtr.All)
			// restore the captured piece to the hash (excluding e.p.)
			b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex+(int(capturedPieceType)-1)][m.To()]
		}

		// Restore rooks from castling move
		if castleStatus != 0 {
			b.movePiece(Rook, Rook, newRookLoc, oldRookLoc, &ourBitboardPtr.Rooks, &ourBitboardPtr.Rooks, &ourBitboardPtr.All) // ??? Flumoxed
			// Revert castling rook move
			b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(Rook-1)][oldRookLoc]
			b.hash ^= pieceSquareZobristC[ourPiecesPawnZobristIndex+(Rook-1)][newRookLoc]
		}

		// Unapply en-passant square change, and capture if necessary
		b.hash ^= uint64(b.enpassant)        // undo the new en passant square from the hash
		b.hash ^= uint64(oldEpCaptureSquare) // restore the old one to the hash
		b.enpassant = oldEpCaptureSquare
		if actuallyPerformedEpCapture {
			epOpponentPawnLocation := uint8(int8(oldEpCaptureSquare) + epDelta)
			b.addPiece(Pawn, epOpponentPawnLocation, &oppBitboardPtr.Pawns, &oppBitboardPtr.All)
			// Add the opponent pawn to the board hash.
			b.hash ^= pieceSquareZobristC[oppPiecesPawnZobristIndex][epOpponentPawnLocation]
		}

		// Decrement move clock
		if /*!b.Wtomove*/b.Colortomove == Black {
			b.Fullmoveno-- // decrement after undoing black's move
		}

		// Restore castling flags
		// Must update castling flags AFTER turn swap
		if flippedKsCastle {
			b.flipKingsideCastle()
		}
		if flippedQsCastle {
			b.flipQueensideCastle()
		}
		if flippedOppKsCastle {
			b.flipOppKingsideCastle()
		}
		if flippedOppQsCastle {
			b.flipOppQueensideCastle()
		}
	}
	
	return &moveApplication
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
