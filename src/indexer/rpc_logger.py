import logging

logger = logging.getLogger("flare.indexer.rpc")

DEFAULT_COSTON2_LOG_RANGE = 30

def handle_get_logs_error(error: Exception, requested_range: int):
    error_msg = str(error)
    if "requested too many blocks" in error_msg or "maximum is set to" in error_msg:
        logger.warning(
            f"RPC getLogs cap exceeded (requested log_range={requested_range}). "
            f"Public Coston2 RPC caps range to {DEFAULT_COSTON2_LOG_RANGE}. Error: {error_msg}"
        )
    else:
        logger.error(f"RPC getLogs failed: {error_msg}")
