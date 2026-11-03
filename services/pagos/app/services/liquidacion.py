"""Registro y liquidacion de apuestas (RF-12, RF-14, RNF-20).

Individuales: mueven tokens. Saldo, apuesta y ledger son atomicos.
Torneo: se pagan en fichas, que viven en Redis (HU2-11). Aqui solo queda la
fila de bets (para el historial y los reportes); no tocan wallets ni el ledger.

Las dos exigen el mercado "abierto" dentro de la misma transaccion (RNF-19).
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
    BET_TORNEO,
    BET_WON,
    LEDGER_COMPLETED,
    LEDGER_DEBIT,
    LEDGER_SETTLE,
    MARKET_ABIERTO,
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


class MercadoCerrado(Exception):
    """El mercado no está abierto: ya no recibe apuestas (RNF-19)."""


class ModoNoSoportado(Exception):
    """La apuesta es del otro modo (individual en tokens, torneo en fichas)."""


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
class ResultadoTorneo:
    apuesta: Bet
    nueva: bool


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


def _mercado_abierto(db: Session, market_id: uuid.UUID) -> Market:
    """Lee el mercado con FOR SHARE: /close (FOR UPDATE) espera a que esta transaccion termine."""
    mercado = db.scalar(select(Market).where(Market.id == market_id).with_for_update(read=True))
    if mercado is None:
        db.rollback()
        raise MercadoNoExiste()
    if mercado.status != MARKET_ABIERTO:
        db.rollback()
        raise MercadoCerrado()
    return mercado


def _debito_existente(db: Session, movimiento: LedgerEntry, datos: BetDebitData) -> ResultadoDebito:
    # lap no cuenta: el Motor la lee en cada intento y un reintento puede caer
    # en otra vuelta. Se conserva la del primer intento.
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
    ):
        raise ClaveReutilizada()
    return ResultadoDebito(apuesta=apuesta, saldo=_saldo(db, datos.user_id), nueva=False)


def debitar_apuesta(db: Session, datos: BetDebitData) -> ResultadoDebito:
    """Debita los tokens y registra la apuesta individual en una sola transaccion (RNF-11)."""
    previo = db.scalar(select(LedgerEntry).where(LedgerEntry.idempotency_key == datos.idempotency_key))
    if previo is not None:
        return _debito_existente(db, previo, datos)

    mercado = _mercado_abierto(db, datos.market_id)

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


def _misma_apuesta_torneo(apuesta: Bet, datos: BetDebitData) -> bool:
    """Sin lap, igual que el debito: un reintento puede llegar en otra vuelta."""
    return (
        apuesta.mode == BET_TORNEO
        and apuesta.user_id == datos.user_id
        and apuesta.market_id == datos.market_id
        and apuesta.option == datos.option
        and apuesta.amount == datos.amount
        and apuesta.multiplier == datos.multiplier
    )


def _torneo_existente(db: Session, datos: BetDebitData) -> ResultadoTorneo | None:
    previa = db.get(Bet, datos.bet_id)
    if previa is None:
        return None
    if not _misma_apuesta_torneo(previa, datos):
        raise ClaveReutilizada()
    return ResultadoTorneo(previa, nueva=False)


def registrar_apuesta_torneo(db: Session, datos: BetDebitData) -> ResultadoTorneo:
    """Guarda la apuesta de torneo en bets (RF-12, RNF-20). No mueve tokens ni escribe en el ledger.

    bet_id es la clave de idempotencia: el mismo bet_id con los mismos datos
    devuelve la apuesta guardada, aunque el mercado ya haya cerrado.
    """
    previa = _torneo_existente(db, datos)
    if previa is not None:
        return previa

    mercado = _mercado_abierto(db, datos.market_id)
    apuesta = Bet(
        id=datos.bet_id,
        user_id=datos.user_id,
        market_id=datos.market_id,
        event_id=mercado.event_id,
        mode=BET_TORNEO,
        option=datos.option,
        amount=datos.amount,
        multiplier=datos.multiplier,
        lap=datos.lap,
        status=BET_PENDING,
    )
    db.add(apuesta)
    try:
        db.commit()
    except IntegrityError:
        db.rollback()
        previa = _torneo_existente(db, datos)
        if previa is not None:
            return previa
        if db.scalar(select(Bet.id).where(Bet.user_id == datos.user_id, Bet.market_id == datos.market_id)):
            raise ApuestaDuplicada() from None
        raise
    db.refresh(apuesta)
    return ResultadoTorneo(apuesta, nueva=True)


def liquidar_apuesta_torneo(db: Session, bet_id: uuid.UUID, resultado: str) -> ResultadoTorneo:
    """Marca won/lost y el pago en fichas (monto × multiplicador, truncado). Sin ledger.

    Las fichas se acreditan en Redis (HU2-11); esto solo deja el registro.
    Repetir con el mismo resultado no cambia nada; con otro resultado, ClaveReutilizada.
    """
    if resultado not in (BET_WON, BET_LOST):
        raise ResultadoInvalido()
    apuesta = db.scalar(select(Bet).where(Bet.id == bet_id).with_for_update())
    if apuesta is None:
        raise ApuestaNoExiste()
    if apuesta.mode != BET_TORNEO:
        raise ModoNoSoportado()
    if apuesta.status != BET_PENDING:
        previo = apuesta.status
        db.rollback()  # suelta el FOR UPDATE
        if previo != resultado:
            raise ClaveReutilizada()
        return ResultadoTorneo(apuesta, nueva=False)

    apuesta.status = resultado
    apuesta.payout = pago_por_apuesta(apuesta.amount, apuesta.multiplier) if resultado == BET_WON else 0
    db.commit()
    db.refresh(apuesta)
    return ResultadoTorneo(apuesta, nueva=True)
