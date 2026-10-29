"""Debito y liquidacion de apuestas individuales; saldo, apuesta y ledger son atomicos.

Solo apuestas individuales: son las que mueven tokens (RF-12, RF-14). Las de
torneo se pagan en fichas, que viven en Redis y no pasan por el ledger.
"""
import uuid
from dataclasses import dataclass
from decimal import ROUND_DOWN, Decimal

from sqlalchemy import select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.models import (
    BET_INDIVIDUAL,
    BET_LOST,
    BET_PENDING,
    BET_WON,
    LEDGER_COMPLETED,
    LEDGER_DEBIT,
    LEDGER_SETTLE,
    Bet,
    LedgerEntry,
    Market,
    Wallet,
)


class ApuestaNoExiste(Exception):
    """No se encontró la apuesta solicitada."""


class ApuestaDuplicada(Exception):
    """El usuario ya apostó en ese mercado (RF-12: una sola apuesta por usuario y mercado)."""


class BilleteraNoExiste(Exception):
    """El usuario no tiene billetera."""


class ClaveReutilizada(Exception):
    """La clave de idempotencia ya identifica otro contenido."""


class MercadoNoExiste(Exception):
    """No se encontró el mercado de la apuesta."""


class ModoNoSoportado(Exception):
    """La apuesta es de torneo: no se liquida en tokens."""


class ResultadoInvalido(Exception):
    """El resultado no es won ni lost."""


class SaldoInsuficiente(Exception):
    """El usuario no tiene saldo suficiente para la apuesta."""


@dataclass(frozen=True)
class BetDebitData:
    bet_id: uuid.UUID
    user_id: uuid.UUID
    market_id: uuid.UUID
    option: str
    amount: int
    multiplier: Decimal
    lap: int | None
    idempotency_key: str


@dataclass(frozen=True)
class ResultadoDebito:
    apuesta: Bet
    saldo: int
    nueva: bool


@dataclass(frozen=True)
class ResultadoLiquidacion:
    apuesta: Bet
    saldo: int
    nueva: bool


def _saldo(db: Session, user_id: uuid.UUID) -> int:
    return db.scalar(select(Wallet.balance).where(Wallet.user_id == user_id))


def _debito_existente(db: Session, movimiento: LedgerEntry, datos: BetDebitData) -> ResultadoDebito:
    apuesta = db.get(Bet, datos.bet_id)
    if (
        movimiento.type != LEDGER_DEBIT
        or movimiento.status != LEDGER_COMPLETED
        or movimiento.bet_id != datos.bet_id
        or movimiento.user_id != datos.user_id
        or movimiento.tokens_delta != -datos.amount
        or apuesta is None
        or apuesta.user_id != datos.user_id
        or apuesta.market_id != datos.market_id
        or apuesta.option != datos.option
        or apuesta.amount != datos.amount
        or apuesta.multiplier != datos.multiplier
        or apuesta.lap != datos.lap
    ):
        raise ClaveReutilizada()
    return ResultadoDebito(apuesta=apuesta, saldo=_saldo(db, datos.user_id), nueva=False)


def debitar_apuesta(db: Session, datos: BetDebitData) -> ResultadoDebito:
    """Debita los tokens y registra la apuesta individual en una sola transaccion (RNF-11)."""
    previo = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == datos.idempotency_key))
    if previo is not None:
        return _debito_existente(db, previo, datos)

    mercado = db.get(Market, datos.market_id)
    if mercado is None:
        raise MercadoNoExiste()

    # UPDATE atomico: dos apuestas simultaneas no gastan el mismo saldo.
    saldo = db.execute(
        update(Wallet)
        .where(Wallet.user_id == datos.user_id, Wallet.balance >= datos.amount)
        .values(balance=Wallet.balance - datos.amount)
        .returning(Wallet.balance)
    ).scalar_one_or_none()
    if saldo is None:
        db.rollback()
        if db.get(Wallet, datos.user_id) is None:
            raise BilleteraNoExiste()
        raise SaldoInsuficiente()

    apuesta = Bet(
        id=datos.bet_id,
        user_id=datos.user_id,
        market_id=datos.market_id,
        event_id=mercado.event_id,
        mode=BET_INDIVIDUAL,
        option=datos.option,
        amount=datos.amount,
        multiplier=datos.multiplier,
        lap=datos.lap,
        status=BET_PENDING,
    )
    db.add(apuesta)
    try:
        db.flush()
        db.add(
            LedgerEntry(
                user_id=datos.user_id,
                bet_id=datos.bet_id,
                event_id=mercado.event_id,
                type=LEDGER_DEBIT,
                tokens_delta=-datos.amount,
                status=LEDGER_COMPLETED,
                idempotency_key=datos.idempotency_key,
            )
        )
        db.commit()
    except IntegrityError:
        db.rollback()
        previo = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == datos.idempotency_key))
        if previo is not None:
            return _debito_existente(db, previo, datos)
        if db.get(Bet, datos.bet_id) is not None:
            raise ClaveReutilizada() from None
        if db.scalar(select(Bet.id).where(Bet.user_id == datos.user_id, Bet.market_id == datos.market_id)):
            raise ApuestaDuplicada() from None
        raise
    db.refresh(apuesta)
    return ResultadoDebito(apuesta=apuesta, saldo=saldo, nueva=True)


def pago_por_apuesta(monto: int, multiplicador: Decimal) -> int:
    """Monto × multiplicador (incluye lo apostado, RF-14). Los tokens son enteros: la fraccion se trunca."""
    return int((Decimal(monto) * multiplicador).to_integral_value(rounding=ROUND_DOWN))


def liquidar_apuesta(db: Session, bet_id: uuid.UUID, resultado: str, idempotency_key: str) -> ResultadoLiquidacion:
    """Resuelve una apuesta individual y paga si acerto, una sola vez (RNF-12)."""
    if resultado not in (BET_WON, BET_LOST):
        raise ResultadoInvalido()
    apuesta = db.scalar(select(Bet).where(Bet.id == bet_id).with_for_update())
    if apuesta is None:
        raise ApuestaNoExiste()
    if apuesta.mode != BET_INDIVIDUAL:
        raise ModoNoSoportado()
    pago = pago_por_apuesta(apuesta.amount, apuesta.multiplier) if resultado == BET_WON else 0

    previo = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == idempotency_key))
    if previo is not None:
        if (
            previo.type != LEDGER_SETTLE
            or previo.status != LEDGER_COMPLETED
            or previo.bet_id != bet_id
            or previo.user_id != apuesta.user_id
            or previo.tokens_delta != pago
            or apuesta.status != resultado
        ):
            raise ClaveReutilizada()
        return ResultadoLiquidacion(apuesta, _saldo(db, apuesta.user_id), nueva=False)

    if apuesta.status != BET_PENDING:
        raise ClaveReutilizada()

    # Tambien una apuesta perdida deja su fila (0 tokens): la clave unica es lo
    # que impide liquidarla dos veces.
    db.add(
        LedgerEntry(
            user_id=apuesta.user_id,
            bet_id=bet_id,
            event_id=apuesta.event_id,
            type=LEDGER_SETTLE,
            tokens_delta=pago,
            status=LEDGER_COMPLETED,
            idempotency_key=idempotency_key,
        )
    )
    apuesta.status = resultado
    apuesta.payout = pago
    if pago:
        saldo = db.execute(
            update(Wallet)
            .where(Wallet.user_id == apuesta.user_id)
            .values(balance=Wallet.balance + pago)
            .returning(Wallet.balance)
        ).scalar_one_or_none()
        if saldo is None:
            db.rollback()
            raise BilleteraNoExiste()
    db.commit()
    db.refresh(apuesta)
    return ResultadoLiquidacion(apuesta, _saldo(db, apuesta.user_id), nueva=True)
