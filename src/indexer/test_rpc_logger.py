import logging
from src.indexer.rpc_logger import handle_get_logs_error, DEFAULT_COSTON2_LOG_RANGE

def test_handle_get_logs_exceeded_warning(caplog):
    with caplog.at_level(logging.WARNING):
        exc = Exception("requested too many blocks from 33550030 to 33550130, maximum is set to 30")
        handle_get_logs_error(exc, 1000)
    
    assert "RPC getLogs cap exceeded" in caplog.text
    assert f"{DEFAULT_COSTON2_LOG_RANGE}" in caplog.text
