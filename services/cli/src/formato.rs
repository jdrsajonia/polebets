//! Formato de números y horas para la pantalla (es-CO).

/// Hora de Colombia respecto a UTC (no tiene horario de verano).
const DESFASE_COLOMBIA_S: i64 = -5 * 3600;

/// Separa miles con punto: 1234567 → "1.234.567".
pub fn miles(n: i64) -> String {
    let digitos = n.unsigned_abs().to_string();
    let mut con_puntos = String::new();
    for (i, c) in digitos.chars().enumerate() {
        if i > 0 && (digitos.len() - i).is_multiple_of(3) {
            con_puntos.push('.');
        }
        con_puntos.push(c);
    }
    if n < 0 {
        format!("-{con_puntos}")
    } else {
        con_puntos
    }
}

/// Ganancia con signo explícito: "+1.200", "-300", "0".
pub fn con_signo(n: i64) -> String {
    if n > 0 {
        format!("+{}", miles(n))
    } else {
        miles(n)
    }
}

/// Días desde 1970-01-01 de una fecha civil (algoritmo de Howard Hinnant).
fn dias_desde_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

/// Fecha civil de unos días desde 1970-01-01 (inversa de `dias_desde_civil`).
fn civil_desde_dias(z: i64) -> (i64, i64, i64) {
    let z = z + 719468;
    let era = z.div_euclid(146097);
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { yoe + era * 400 + 1 } else { yoe + era * 400 }, m, d)
}

/// Pasa un ISO 8601 en UTC ("2026-11-03T20:45:59.955Z") a la fecha y hora de
/// Colombia: (año, mes, día, hora, minuto, segundo).
fn a_colombia(iso: &str) -> Option<(i64, i64, i64, i64, i64, i64)> {
    let campo = |desde: usize, hasta: usize| iso.get(desde..hasta)?.parse::<i64>().ok();
    let (y, m, d) = (campo(0, 4)?, campo(5, 7)?, campo(8, 10)?);
    let (hh, mm, ss) = (campo(11, 13)?, campo(14, 16)?, campo(17, 19)?);
    let total = (dias_desde_civil(y, m, d) * 86400 + hh * 3600 + mm * 60 + ss) + DESFASE_COLOMBIA_S;
    let (y, m, d) = civil_desde_dias(total.div_euclid(86400));
    let s = total.rem_euclid(86400);
    Some((y, m, d, s / 3600, s % 3600 / 60, s % 60))
}

/// "2026-11-03T20:45:59Z" → "15:45:59" (hora de Colombia).
pub fn hora_colombia(iso: &str) -> Option<String> {
    let (_, _, _, hh, mm, ss) = a_colombia(iso)?;
    Some(format!("{hh:02}:{mm:02}:{ss:02}"))
}

/// "2026-11-03T20:45:59Z" → "03/11 15:45" (hora de Colombia).
pub fn fecha_hora_colombia(iso: &str) -> Option<String> {
    let (_, m, d, hh, mm, _) = a_colombia(iso)?;
    Some(format!("{d:02}/{m:02} {hh:02}:{mm:02}"))
}

#[cfg(test)]
mod pruebas {
    use super::*;

    #[test]
    fn separa_miles_con_punto() {
        assert_eq!(miles(0), "0");
        assert_eq!(miles(999), "999");
        assert_eq!(miles(50_000), "50.000");
        assert_eq!(miles(-1_234_567), "-1.234.567");
        assert_eq!(con_signo(1200), "+1.200");
        assert_eq!(con_signo(-300), "-300");
    }

    #[test]
    fn convierte_utc_a_hora_de_colombia() {
        assert_eq!(
            hora_colombia("2026-11-03T20:45:59.955000Z").as_deref(),
            Some("15:45:59")
        );
        // Pasada la medianoche UTC sigue siendo el día anterior en Colombia.
        assert_eq!(
            fecha_hora_colombia("2026-03-01T02:10:00Z").as_deref(),
            Some("28/02 21:10")
        );
        assert_eq!(
            fecha_hora_colombia("2027-01-01T04:59:00Z").as_deref(),
            Some("31/12 23:59")
        );
        assert_eq!(hora_colombia("no es fecha"), None);
    }
}
