Aaruni Aggarwal  [9:00 AM]
Hi All, I am trying to perform LM-Eval model evaluation from RHOAI3.0 UI and facing below error when Make model deployment available through an external route is enabled while deploying the model. It works fine when external route is disabled.
error:
[root@rdr-rhoai-comp-bastion-0 ~]# oc get pods
NAME                                    READY   STATUS                  RESTARTS   AGE
metallama-predictor-5bd878bb6d-hb4mk    1/1     Running                 0          7d1h
tinyllama-predictor-7cb5fbc4b-4cw5b     1/1     Running                 0          9d
tinyllama-run                           0/1     Error                   0          3m14s

[root@rdr-rhoai-comp-bastion-0 ~]# oc logs pod/tinyllama-run 
Defaulted container "main" out of: main, driver (init)
2025-12-08:07:00:18,580 INFO     [__main__:325] Including path: /opt/app-root/src/my_tasks
2025-12-08:07:00:31,798 INFO     [__main__:397] Selected Tasks: ['arc_easy']
2025-12-08:07:00:31,799 INFO     [lm_eval.evaluator:177] Setting random seed to 0 | Setting numpy seed to 1234 | Setting torch manual seed to 1234 | Setting fewshot manual seed to 1234
2025-12-08:07:00:31,800 INFO     [lm_eval.evaluator:214] Initializing local-completions model, with arguments: {'model': 'tinyllama', 'base_url': 'https://tinyllama-model-deploy.apps.rdr-rhoai-comp.ibm.com/v1/completions', 'num_concurrent': 1, 'max_retries': 3, 'tokenized_requests': True, 'tokenizer': 'TinyLlama/TinyLlama-1.1B-Chat-v1.0'}
2025-12-08:07:00:31,800 INFO     [lm_eval.models.api_models:115] Using max length 2048 - 1
2025-12-08:07:00:31,800 INFO     [lm_eval.models.api_models:118] Concurrent requests are disabled. To enable concurrent requests, set `num_concurrent` > 1.
2025-12-08:07:00:31,800 INFO     [lm_eval.models.api_models:133] Using tokenizer huggingface
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2025-12-08:07:00:31,962 WARNING  [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2025-12-08:07:00:36,573 WARNING  [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2025-12-08:07:00:36,653 WARNING  [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2025-12-08:07:00:36,739 WARNING  [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Generating train split:   0%|          | 0/2251 [00:00<?, ? examples/s]
Generating train split: 100%|██████████| 2251/2251 [00:00<00:00, 386704.01 examples/s]
Generating test split:   0%|          | 0/2376 [00:00<?, ? examples/s]
Generating test split: 100%|██████████| 2376/2376 [00:00<00:00, 574059.12 examples/s]
Generating validation split:   0%|          | 0/570 [00:00<?, ? examples/s]
Generating validation split: 100%|██████████| 570/570 [00:00<00:00, 254389.58 examples/s]
2025-12-08:07:00:36,973 INFO     [lm_eval.api.task:420] Building contexts for arc_easy on rank 0...
  0%|          | 0/2376 [00:00<?, ?it/s]
  6%|▌         | 141/2376 [00:00<00:01, 1405.04it/s]
 12%|█▏        | 282/2376 [00:00<00:01, 1405.17it/s]
 18%|█▊        | 423/2376 [00:00<00:01, 1377.83it/s]
 24%|██▎       | 561/2376 [00:00<00:01, 1341.99it/s]
 29%|██▉       | 696/2376 [00:00<00:01, 1291.15it/s]
 35%|███▍      | 831/2376 [00:00<00:01, 1308.97it/s]
 41%|████      | 966/2376 [00:00<00:01, 1320.33it/s]
 46%|████▋     | 1099/2376 [00:00<00:00, 1309.05it/s]
 52%|█████▏    | 1231/2376 [00:00<00:00, 1265.96it/s]
 58%|█████▊    | 1374/2376 [00:01<00:00, 1313.61it/s]
 64%|██████▍   | 1518/2376 [00:01<00:00, 1349.66it/s]
 70%|██████▉   | 1661/2376 [00:01<00:00, 1372.98it/s]
 76%|███████▌  | 1804/2376 [00:01<00:00, 1388.61it/s]
 82%|████████▏ | 1946/2376 [00:01<00:00, 1395.34it/s]
 88%|████████▊ | 2090/2376 [00:01<00:00, 1405.80it/s]
 94%|█████████▍| 2231/2376 [00:01<00:00, 1403.80it/s]
100%|█████████▉| 2372/2376 [00:01<00:00, 1299.40it/s]
100%|██████████| 2376/2376 [00:01<00:00, 1338.53it/s]
2025-12-08:07:00:38,910 INFO     [lm_eval.evaluator:525] Running loglikelihood requests
Requesting API:   0%|          | 0/9501 [00:00<?, ?it/s]
Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 467, in _make_request
    self._validate_conn(conn)
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 1096, in _validate_conn
    conn.connect()
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 642, in connect
    sock_and_verified = _ssl_wrap_socket_and_match_hostname(
                        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 782, in _ssl_wrap_socket_and_match_hostname
    ssl_sock = ssl_wrap_socket(
               ^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 470, in ssl_wrap_socket
    ssl_sock = _ssl_wrap_socket_impl(sock, context, tls_in_tls, server_hostname)
               ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 514, in _ssl_wrap_socket_impl
    return ssl_context.wrap_socket(sock, server_hostname=server_hostname)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 517, in wrap_socket
    return self.sslsocket_class._create(
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 1104, in _create
    self.do_handshake()
  File "/usr/lib64/python3.11/ssl.py", line 1382, in do_handshake
    self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1006)

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 790, in urlopen
    response = self._make_request(
               ^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 491, in _make_request
    raise new_e
urllib3.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1006)

The above exception was the direct cause of the following exception:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 667, in send
    resp = conn.urlopen(
           ^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 844, in urlopen
    retries = retries.increment(
              ^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/retry.py", line 515, in increment
    raise MaxRetryError(_pool, url, reason) from reason  # type: ignore[arg-type]
    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
urllib3.exceptions.MaxRetryError: HTTPSConnectionPool(host='tinyllama-model-deploy.apps.rdr-rhoai-comp.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1006)')))

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "<frozen runpy>", line 198, in _run_module_as_main
  File "<frozen runpy>", line 88, in _run_code
  File "/opt/app-root/src/lm_eval/__main__.py", line 486, in <module>
    cli_evaluate()
  File "/opt/app-root/src/lm_eval/__main__.py", line 407, in cli_evaluate
    results = evaluator.simple_evaluate(
              ^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/src/lm_eval/utils.py", line 423, in _wrapper
    return fn(*args, **kwargs)
           ^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/src/lm_eval/evaluator.py", line 316, in simple_evaluate
    results = evaluate(
              ^^^^^^^^^
..
.. 
        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 698, in send
    raise SSLError(e, request=request)
requests.exceptions.SSLError: HTTPSConnectionPool(host='tinyllama-model-deploy.apps.rdr-rhoai-comp.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1006)')))
Requesting API:   0%|          | 0/9501 [00:02<?, ?it/s]
2025-12-08T07:00:43Z	INFO	driver	update status: job completed	{"state": {"state":"Complete","reason":"Failed","message":"exit status 1","progressBars":[{"message":"Generating train split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2251/2251"},{"message":"Generating test split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2376/2376"},{"message":"Generating validation split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"570/570"}]}}
2025-12-08T07:00:46Z	ERROR	driver	Driver.Run failed	{"error": "exit status 1"}
main.main
	/go/src/github.com/trustyai-explainability/trustyai-service-operator/cmd/lmes_driver/main.go:148
runtime.main
	/usr/lib/golang/src/runtime/proc.go:272Any pointers would be helpful.
Thanks (edited) 
Aaruni Aggarwal  [9:03 AM]
I believe, Editing the LMevalJob by adding following should help, but I can't seem to find any option in UI to add this while creating Evaluation.
  name: verify_certificate
  value: "False"(edited)
Aaruni Aggarwal  [9:07 AM]
Is there any additional configuration that needs to be enabled ?
I have enabled following in DSCcluster:
trustyai:
      eval:
        lmeval:
          permitCodeExecution: allow
          permitOnline: allow
      managementState: Managed(edited)
tinyllama-eval-ui-form.png Aaruni Aggarwal  [9:53 AM]
Also, is there any possibilty to add some arguments via UI while creating LMevalJob? For eg. set limits in case of larger dataset. As currently it takes too long for the evaluation.
rui  [9:55 AM]
@Aaruni Aggarwal thanks for the heads up. Looking into this.
In the meantime cc @Daragh for the UI suggestions.
Aaruni Aggarwal  [11:22 AM]
Thank you so much @rui for looking into it.
Dipanshu Gupta  [6:47 AM]
cc @Purva Naik @Emmanuel Ikeola
Aaruni Aggarwal  [3:28 PM]
Hi @rui
May I know if there is any update on the above issue?
Thankstsambari  [7:21 AM]
@rui, Can you please help @Aaruni Aggarwal here?
Aaruni Aggarwal  [2:06 PM]
Hi @rui
May I know if there is any update on the above issue?
Thankstsambari  [3:25 PM]
@lmcfadde
lmcfadde  [2:35 AM]
@rui this is the message I was referring to in my DM.
rui  [11:36 AM]
Apologies for the delay. This issue seems to be two-fold:

There was an issue the verify_certificate that only manifested with batch requests. A fix is being merged (tracked in here)
In addition, to use custom certs, they have to be mounted via the CR as documented in https://trustyai.org/docs/main/lmeval-tls-certificates
:jira: RHOAIENG-44351 [Review] : [LMEval] Batch requests not using provided SSL certsAdded by assisted-installer-jira-unfurlerAaruni Aggarwal  [12:44 PM]
Hi Rui
Thanks for responding. Is it possible to set verify_certificate: False while creating an evaluation run?
rui  [12:46 PM]
verify_certificate: False will work for non-batched. The fix above will make it work for all cases (non-batch and batched).
Aaruni Aggarwal  [12:54 PM]
May I know in which RHOAI version fix will be available?
rui  [12:58 PM]
The fix is expected for 3.3 (and potentially 2.25.2 pending discussion, confirmed) (edited) 
Aaruni Aggarwal  [1:14 PM]
sure, thanks Rui
Aaruni Aggarwal  [10:56 AM]
Hi Rui,
I found out that the PR which you have raised got merged and hence I validated it again on RHOAI3.3 but still hitting the same SSLcert error when external route is enabled while deploying the model.
[root@rdr-rhoai-comp-bastion-0 ~]# oc get pods -n model-eval
NAME                                      READY   STATUS      RESTARTS   AGE
tiny-external                             0/1     Error       0          2m43s
tinyeval                                  0/1     Completed   0          51d
tinyevallama-predictor-5b599cdcdb-jnlqb   1/1     Running     1          51d
tinyllama-predictor-5b6798d8c9-9w4kb      1/1     Running     0          9m40s

[root@rdr-rhoai-comp-bastion-0 ~]# oc logs -f pod/tiny-external -n model-eval
Defaulted container "main" out of: main, driver (init)
2026-01-30:06:31:14,632 INFO     [__main__:325] Including path: /opt/app-root/src/my_tasks
2026-01-30:06:31:29,946 INFO     [__main__:397] Selected Tasks: ['arc_easy']
2026-01-30:06:31:29,949 INFO     [lm_eval.evaluator:177] Setting random seed to 0 | Setting numpy seed to 1234 | Setting torch manual seed to 1234 | Setting fewshot manual seed to 1234
2026-01-30:06:31:29,949 INFO     [lm_eval.evaluator:214] Initializing local-completions model, with arguments: {'model': 'tinyllama', 'base_url': 'https://tinyllama-model-eval.apps.rdr-rhoai-comp.ibm.com/v1/completions', 'num_concurrent': 1, 'max_retries': 3, 'tokenized_requests': True, 'tokenizer': 'TinyLlama/TinyLlama-1.1B-Chat-v1.0'}
2026-01-30:06:31:29,949 INFO     [lm_eval.models.api_models:115] Using max length 2048 - 1
2026-01-30:06:31:29,949 INFO     [lm_eval.models.api_models:118] Concurrent requests are disabled. To enable concurrent requests, set `num_concurrent` > 1.
2026-01-30:06:31:29,949 INFO     [lm_eval.models.api_models:133] Using tokenizer huggingface
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-01-30:06:31:33,349 WARNING  [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Generating train split:   0%|          | 0/2251 [00:00<?, ? examples/s]
Generating train split: 100%|██████████| 2251/2251 [00:00<00:00, 287908.34 examples/s]
Generating test split:   0%|          | 0/2376 [00:00<?, ? examples/s]
Generating test split: 100%|██████████| 2376/2376 [00:00<00:00, 371077.83 examples/s]
Generating validation split:   0%|          | 0/570 [00:00<?, ? examples/s]
Generating validation split: 100%|██████████| 570/570 [00:00<00:00, 203954.38 examples/s]
2026-01-30:06:31:33,602 INFO     [lm_eval.api.task:420] Building contexts for arc_easy on rank 0...
  0%|          | 0/2376 [00:00<?, ?it/s]
  5%|▌         | 124/2376 [00:00<00:01, 1239.49it/s]
 11%|█         | 250/2376 [00:00<00:01, 1248.61it/s]
 16%|█▌        | 375/2376 [00:00<00:01, 1198.55it/s]
 21%|██        | 503/2376 [00:00<00:01, 1227.35it/s]
 26%|██▋       | 626/2376 [00:00<00:01, 1208.00it/s]
 31%|███▏      | 747/2376 [00:00<00:01, 1200.46it/s]
 37%|███▋      | 868/2376 [00:00<00:01, 1176.48it/s]
 41%|████▏     | 986/2376 [00:00<00:01, 1162.40it/s]
 46%|████▋     | 1103/2376 [00:00<00:01, 1160.50it/s]
 51%|█████▏    | 1220/2376 [00:01<00:01, 1150.93it/s]
 56%|█████▌    | 1336/2376 [00:01<00:00, 1134.48it/s]
 61%|██████    | 1450/2376 [00:01<00:00, 1121.67it/s]
 66%|██████▌   | 1563/2376 [00:01<00:00, 1121.08it/s]
 71%|███████   | 1676/2376 [00:01<00:00, 755.00it/s] 
 74%|███████▍  | 1769/2376 [00:01<00:00, 792.32it/s]
 79%|███████▉  | 1888/2376 [00:01<00:00, 886.33it/s]
 84%|████████▍ | 1994/2376 [00:01<00:00, 929.85it/s]
 89%|████████▊ | 2108/2376 [00:02<00:00, 984.47it/s]
 94%|█████████▎| 2224/2376 [00:02<00:00, 1031.79it/s]
 98%|█████████▊| 2335/2376 [00:02<00:00, 1052.69it/s]
100%|██████████| 2376/2376 [00:02<00:00, 1056.00it/s]
2026-01-30:06:31:36,031 INFO     [lm_eval.evaluator:525] Running loglikelihood requests
Requesting API:   0%|          | 0/9501 [00:00<?, ?it/s]
Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 464, in _make_request
    self._validate_conn(conn)
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 1093, in _validate_conn
    conn.connect()
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 796, in connect
    sock_and_verified = _ssl_wrap_socket_and_match_hostname(
                        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 975, in _ssl_wrap_socket_and_match_hostname
    ssl_sock = ssl_wrap_socket(
               ^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 483, in ssl_wrap_socket
    ssl_sock = _ssl_wrap_socket_impl(sock, context, tls_in_tls, server_hostname)
               ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 527, in _ssl_wrap_socket_impl
    return ssl_context.wrap_socket(sock, server_hostname=server_hostname)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 517, in wrap_socket
    return self.sslsocket_class._create(
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 1104, in _create
    self.do_handshake()
  File "/usr/lib64/python3.11/ssl.py", line 1382, in do_handshake
    self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 787, in urlopen
    response = self._make_request(
               ^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 488, in _make_request
    raise new_e
urllib3.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)

The above exception was the direct cause of the following exception:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 644, in send
    resp = conn.urlopen(
           ^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 841, in urlopen
    retries = retries.increment(
              ^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/retry.py", line 519, in increment
    raise MaxRetryError(_pool, url, reason) from reason  # type: ignore[arg-type]
    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
urllib3.exceptions.MaxRetryError: HTTPSConnectionPool(host='tinyllama-model-eval.apps.rdr-rhoai-comp.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
..
..
  File "/opt/app-root/lib64/python3.11/site-packages/requests/api.py", line 59, in request
    return session.request(method=method, url=url, **kwargs)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/requests/sessions.py", line 589, in request
    resp = self.send(prep, **send_kwargs)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/requests/sessions.py", line 703, in send
    r = adapter.send(request, **kwargs)
        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 675, in send
    raise SSLError(e, request=request)
requests.exceptions.SSLError: HTTPSConnectionPool(host='tinyllama-model-eval.apps.rdr-rhoai-comp.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
Requesting API:   0%|          | 0/9501 [00:02<?, ?it/s]I understand that we either need to set verify_certificate=False or provide the certificate path in the LMEvalJob. However, there is currently no option to configure this from the UI when creating an evaluation run. If we update it later via the CLI, the evaluation run would already fail by that point.
 May I know what needs to be done in that case.

I also tried editing the LMevalJob by adding verify_certificate, but I am not sure, how to reconcile the pod. I deleted the pod, thinking job will re-create it but no new pod was created. (edited) 
rui  [11:21 AM]
Hi @Aaruni Aggarwal, IIUC this is happening with the current 3.3 RC? I'll look into it asap.
Aaruni Aggarwal  [11:40 AM]
Thanks for responding Rui.
Initially, I tried with RHOAI3.0 there also it was failing.
And today I have tried on RHOAI3.3
build:
quay.io/rhoai/rhoai-fbc-fragment:rhoai-3.3@sha256:686fceae511cee7c2ab04b25771b9bc0bd53b540f716fae81941d17ab8cf7059 This is the build which got pushed after your PR got merged. This is the build from 17th Jan and the PR got merged on 9th Jan.
aymk  [3:27 PM]
Hi @rui,
 We are also hitting the same error while trying to perform LM-Eval model evaluation from RHOAI3.2 UI:
2026-02-04:15:08:32,865 INFO [__main__:325] Including path: /opt/app-root/src/my_tasks
2026-02-04:15:08:34,471 INFO [__main__:397] Selected Tasks: ['arc_easy']
2026-02-04:15:08:34,473 INFO [lm_eval.evaluator:177] Setting random seed to 0 | Setting numpy seed to 1234 | Setting torch manual seed to 1234 | Setting fewshot manual seed to 1234
2026-02-04:15:08:34,473 INFO [lm_eval.evaluator:214] Initializing local-completions model, with arguments: {'model': 'test', 'base_url': 'https://test-test.apps.ocpz-standard-1.m42lp57.lnxero1.boe/v1/completions', 'num_concurrent': 1, 'max_retries': 3, 'tokenized_requests': True, 'tokenizer': 'Qwen/Qwen2.5-0.5B-Instruct'}
2026-02-04:15:08:34,473 INFO [lm_eval.models.api_models:115] Using max length 2048 - 1
2026-02-04:15:08:34,473 INFO [lm_eval.models.api_models:118] Concurrent requests are disabled. To enable concurrent requests, set `num_concurrent` > 1.
2026-02-04:15:08:34,473 INFO [lm_eval.models.api_models:133] Using tokenizer huggingface
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-02-04:15:08:40,968 WARNING [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-02-04:15:08:41,371 WARNING [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-02-04:15:08:41,645 WARNING [huggingface_hub.file_download:1670] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Generating train split: 0%| | 0/2251 [00:00<?, ? examples/s]
Generating train split: 100%|██████████| 2251/2251 [00:00<00:00, 191995.49 examples/s]
Generating test split: 0%| | 0/2376 [00:00<?, ? examples/s]
Generating test split: 100%|██████████| 2376/2376 [00:00<00:00, 423206.48 examples/s]
Generating validation split: 0%| | 0/570 [00:00<?, ? examples/s]
Generating validation split: 100%|██████████| 570/570 [00:00<00:00, 231796.91 examples/s]
2026-02-04:15:08:41,898 INFO [lm_eval.api.task:420] Building contexts for arc_easy on rank 0...
0%| | 0/2376 [00:00<?, ?it/s]
7%|▋ | 156/2376 [00:00<00:01, 1559.79it/s]
13%|█▎ | 312/2376 [00:00<00:01, 1535.42it/s]
20%|█▉ | 466/2376 [00:00<00:01, 1499.88it/s]
26%|██▌ | 617/2376 [00:00<00:01, 1478.88it/s]
32%|███▏ | 765/2376 [00:00<00:02, 772.26it/s]
39%|███▉ | 923/2376 [00:00<00:01, 939.98it/s]
45%|████▌ | 1080/2376 [00:00<00:01, 1083.66it/s]
52%|█████▏ | 1230/2376 [00:01<00:00, 1184.66it/s]
58%|█████▊ | 1383/2376 [00:01<00:00, 1273.39it/s]
65%|██████▍ | 1533/2376 [00:01<00:00, 1334.12it/s]
71%|███████ | 1679/2376 [00:01<00:00, 1364.91it/s]
77%|███████▋ | 1835/2376 [00:01<00:00, 1418.11it/s]
84%|████████▎ | 1985/2376 [00:01<00:00, 1441.58it/s]
90%|█████████ | 2142/2376 [00:01<00:00, 1477.98it/s]
97%|█████████▋| 2296/2376 [00:01<00:00, 1494.00it/s]
100%|██████████| 2376/2376 [00:01<00:00, 1298.22it/s]
2026-02-04:15:08:43,819 INFO [lm_eval.evaluator:525] Running loglikelihood requests
Requesting API: 0%| | 0/9501 [00:00<?, ?it/s]
Traceback (most recent call last):
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 464, in _make_request
self._validate_conn(conn)
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 1093, in _validate_conn
conn.connect()
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 796, in connect
sock_and_verified = _ssl_wrap_socket_and_match_hostname(
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 975, in _ssl_wrap_socket_and_match_hostname
ssl_sock = ssl_wrap_socket(
^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 483, in ssl_wrap_socket
ssl_sock = _ssl_wrap_socket_impl(sock, context, tls_in_tls, server_hostname)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 527, in _ssl_wrap_socket_impl
return ssl_context.wrap_socket(sock, server_hostname=server_hostname)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/usr/lib64/python3.11/ssl.py", line 517, in wrap_socket
return self.sslsocket_class._create(
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/usr/lib64/python3.11/ssl.py", line 1104, in _create
self.do_handshake()
File "/usr/lib64/python3.11/ssl.py", line 1382, in do_handshake
self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)
During handling of the above exception, another exception occurred:
Traceback (most recent call last):
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 787, in urlopen
response = self._make_request(
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 488, in _make_request
raise new_e
urllib3.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)
The above exception was the direct cause of the following exception:
Traceback (most recent call last):
File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 644, in send
resp = conn.urlopen(
^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 841, in urlopen
retries = retries.increment(
^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/retry.py", line 519, in increment
raise MaxRetryError(_pool, url, reason) from reason # type: ignore[arg-type]
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
urllib3.exceptions.MaxRetryError: HTTPSConnectionPool(host='test-test.apps.ocpz-standard-1.m42lp57.lnxero1.boe', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
During handling of the above exception, another exception occurred:
Traceback (most recent call last):
File "<frozen runpy>", line 198, in _run_module_as_main
File "<frozen runpy>", line 88, in _run_code
File "/opt/app-root/src/lm_eval/__main__.py", line 486, in <module>
cli_evaluate()
File "/opt/app-root/src/lm_eval/__main__.py", line 407, in cli_evaluate
results = evaluator.simple_evaluate(
^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/utils.py", line 423, in _wrapper
return fn(*args, **kwargs)
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/evaluator.py", line 316, in simple_evaluate
results = evaluate(
^^^^^^^^^
File "/opt/app-root/src/lm_eval/utils.py", line 423, in _wrapper
return fn(*args, **kwargs)
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/evaluator.py", line 536, in evaluate
resps = getattr(lm, reqtype)(cloned_reqs)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/api/model.py", line 382, in loglikelihood
return self._loglikelihood_tokens(new_reqs, disable_tqdm=disable_tqdm)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/models/api_models.py", line 538, in _loglikelihood_tokens
outputs = retry(
^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 336, in wrapped_f
return copy(f, *args, **kw)
^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 475, in __call__
do = self.iter(retry_state=retry_state)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 376, in iter
result = action(retry_state)
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 418, in exc_check
raise retry_exc.reraise()
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 185, in reraise
raise self.last_attempt.result()
^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/usr/lib64/python3.11/concurrent/futures/_base.py", line 449, in result
return self.__get_result()
^^^^^^^^^^^^^^^^^^^
File "/usr/lib64/python3.11/concurrent/futures/_base.py", line 401, in __get_result
raise self._exception
File "/opt/app-root/lib64/python3.11/site-packages/tenacity/__init__.py", line 478, in __call__
result = fn(*args, **kwargs)
^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/src/lm_eval/models/api_models.py", line 363, in model_call
response = requests.post(
^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/requests/api.py", line 115, in post
return request("post", url, data=data, json=json, **kwargs)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/requests/api.py", line 59, in request
return session.request(method=method, url=url, **kwargs)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/requests/sessions.py", line 589, in request
resp = self.send(prep, **send_kwargs)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/requests/sessions.py", line 703, in send
r = adapter.send(request, **kwargs)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 675, in send
raise SSLError(e, request=request)
requests.exceptions.SSLError: HTTPSConnectionPool(host='test-test.apps.ocpz-standard-1.m42lp57.lnxero1.boe', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
Requesting API: 0%| | 0/9501 [00:02<?, ?it/s]
2026-02-04T15:08:49Z INFO driver update status: job completed {"state": {"state":"Complete","reason":"Failed","message":"exit status 1","progressBars":[{"message":"Generating train split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2251/2251"},{"message":"Generating test split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2376/2376"},{"message":"Generating validation split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"570/570"}]}}
2026-02-04T15:08:59Z ERROR driver Driver.Run failed {"error": "exit status 1"}
main.main
/go/src/github.com/trustyai-explainability/trustyai-service-operator/cmd/lmes_driver/main.go:150
runtime.main
/usr/lib/golang/src/runtime/proc.go:272cc @satgupta @morana
Aaruni Aggarwal  [8:54 AM]
Hi @rui
May I know if there is any update on the above issue?
Thankstsambari  [1:59 PM]
@rui, The P and z team is till facing this issue post the 3.3 release, can you please take a look and help on this?
lmcfadde  [7:11 PM]
@tsambari @aymk @Aaruni Aggarwal I am thinking this is being tracked in issue https://issues.redhat.com/browse/RHOAIENG-44351?  I cannot see this issue but should it be re-opened and updated with our current findings or that is already done?

Hi @rui do you have enough information to debug this issue which the team is still seeing.  Unsure if my suggestion of updating the issue with these details helps or not.  Let us know if anything is missing or if a debug session would help since we need it for RHOAI 3.4 .
:jira: RHOAIENG-44351 [Testing] : [LMEval] Batch requests not using provided SSL certsAdded by assisted-installer-jira-unfurlertsambari  [10:36 AM]
I do not have access tho this ticket as well.
Aaruni Aggarwal  [1:02 PM]
Hi @rui
Tested LM model eval UI on RHOAI3.4-ea2 . It is still failing with same error.
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 675, in send
    raise SSLError(e, request=request)
requests.exceptions.SSLError: HTTPSConnectionPool(host='tinyllama-model-eval.apps.apar-b-b6fb.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
Requesting API:   0%|          | 0/9501 [00:02<?, ?it/s]
2026-04-09T11:54:23Z	INFO	driver	update status: job completed	{"state": {"state":"Complete","reason":"Failed","message":"exit status 1","progressBars":[{"message":"Generating train split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2251/2251"},{"message":"Generating test split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2376/2376"},{"message":"Generating validation split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"570/570"}]}}
2026-04-09T11:54:28Z	ERROR	driver	Driver.Run failed	{"error": "exit status 1"}
main.main
	/go/src/github.com/trustyai-explainability/trustyai-service-operator/cmd/lmes_driver/main.go:150
runtime.main
	/usr/lib/golang/src/runtime/proc.go:272I have followed this documentation: https://docs.redhat.com/en/documentation/red_hat_openshift_ai_self-managed/3.4/html/[…]aluating_ai_systems/evaluating-large-language-models_evaluate
Aaruni Aggarwal  [1:03 PM]
Sharing pod yaml as well as LMevalJob yaml.
[root@apar-b-b6fb-bastion-0 ~]# oc describe pod tinyllama-predictor-7685b8c98f-lds2t 
Name:             tinyllama-predictor-7685b8c98f-lds2t
Namespace:        model-eval
Priority:         0
Service Account:  aar-model-conn-sa
Node:             worker-1/10.20.188.133
Start Time:       Thu, 09 Apr 2026 07:42:34 -0400
Labels:           app=isvc.tinyllama-predictor
                  component=predictor
                  networking.kserve.io/visibility=exposed
                  opendatahub.io/dashboard=true
                  pod-template-hash=7685b8c98f
                  serving.kserve.io/inferenceservice=tinyllama
Annotations:      internal.serving.kserve.io/storage-initializer-sourceuri: <scheme-placeholder>://tinyllama-1.1b-chat-v1.0
                  internal.serving.kserve.io/storage-spec: true
                  internal.serving.kserve.io/storage-spec-key: aar-model-conn
                  k8s.ovn.org/pod-networks:
                    {"default":{"ip_addresses":["10.131.1.153/23"],"mac_address":"0a:58:0a:83:01:99","gateway_ips":["10.131.0.1"],"routes":[{"dest":"10.128.0....
                  k8s.v1.cni.cncf.io/network-status:
                    [{
                        "name": "ovn-kubernetes",
                        "interface": "eth0",
                        "ips": [
                            "10.131.1.153"
                        ],
                        "mac": "0a:58:0a:83:01:99",
                        "default": true,
                        "dns": {}
                    }]
                  opendatahub.io/connection-path: tinyllama-1.1b-chat-v1.0
                  opendatahub.io/connections: aar-model-conn
                  opendatahub.io/hardware-profile-resource-version: 188659793
                  opendatahub.io/kserve-runtime: vllm
                  opendatahub.io/model-type: generative
                  openshift.io/description: 
                  openshift.io/display-name: tinyllama
                  openshift.io/scc: restricted-v2
                  prometheus.io/path: /metrics
                  prometheus.io/port: 8080
                  seccomp.security.alpha.kubernetes.io/pod: runtime/default
                  security.opendatahub.io/enable-auth: false
                  service.beta.openshift.io/serving-cert-secret-name: tinyllama-predictor-serving-cert
                  serving.kserve.io/deploymentMode: Standard
                  serving.kserve.io/enable-metric-aggregation: false
                  serving.kserve.io/enable-prometheus-scraping: false
                  serving.kserve.io/stop: false
Status:           Running
SeccompProfile:   RuntimeDefault
IP:               10.131.1.153
IPs:
  IP:           10.131.1.153
Controlled By:  ReplicaSet/tinyllama-predictor-7685b8c98f
Init Containers:
  storage-initializer:
    Container ID:  cri-o://f61ac84c91ff46cd6f49f75f5a02016ebd336aae540903f578dabfef9e953a7e
    Image:         registry.redhat.io/rhoai/odh-kserve-storage-initializer-rhel9@sha256:b0cb18b34835e27ee75f8c804c2cf5acdf367c45886c248989588d175052a813
    Image ID:      registry.redhat.io/rhoai/odh-kserve-storage-initializer-rhel9@sha256:7dbe7fe6791aa687e37ef092a5d2a7bdc5e99e6535f463fd0e18fde49f9d4659
    Port:          <none>
    Host Port:     <none>
    Args:
      s3://odh-bucket/models/tinyllama-1.1b-chat-v1.0
      /mnt/models
    State:          Terminated
      Reason:       Completed
      Exit Code:    0
      Started:      Thu, 09 Apr 2026 07:42:35 -0400
      Finished:     Thu, 09 Apr 2026 07:43:02 -0400
    Ready:          True
    Restart Count:  0
    Limits:
      cpu:     1
      memory:  24Gi
    Requests:
      cpu:     100m
      memory:  100Mi
    Environment:
      AWS_CA_BUNDLE_CONFIGMAP:           odh-kserve-custom-ca-bundle
      STORAGE_CONFIG:                    <set to the key 'aar-model-conn' in secret 'storage-config'>  Optional: false
      CA_BUNDLE_CONFIGMAP_NAME:          odh-kserve-custom-ca-bundle
      CA_BUNDLE_VOLUME_MOUNT_POINT:      /etc/ssl/custom-certs
      HF_HUB_ENABLE_HF_TRANSFER:         1
      HF_XET_HIGH_PERFORMANCE:           1
      HF_XET_NUM_CONCURRENT_RANGE_GETS:  8
    Mounts:
      /etc/ssl/custom-certs from cabundle-cert (ro)
      /mnt/models from kserve-provision-location (rw)
Containers:
  kserve-container:
    Container ID:  cri-o://ded62abb27e830f57296ad6ab7245130fbd04eca3069b1c7515156fc9cf6d056
    Image:         registry.redhat.io/rhoai/odh-vllm-cpu-rhel9@sha256:68d4978a9f9ed7e1b1b1001d386e279d546707e49a997680b9dd0232c860b26a
    Image ID:      quay.io/rhoai/odh-vllm-cpu-rhel9@sha256:32825e7e6a745649cb3bb852fbce047fa7e004f7b7b7b9dffccd7e7f69103659
    Port:          8080/TCP
    Host Port:     0/TCP
    Command:
      python
      -m
      vllm.entrypoints.openai.api_server
    Args:
      --port=8080
      --model=/mnt/models
      --served-model-name=tinyllama
      --max-model-len=2048
      --max-num-batched-tokens=2048
      --max-num-seqs=1
    State:          Running
      Started:      Thu, 09 Apr 2026 07:43:02 -0400
    Ready:          True
    Restart Count:  0
    Limits:
      cpu:     8
      memory:  16Gi
    Requests:
      cpu:      8
      memory:   16Gi
    Readiness:  tcp-socket :8080 delay=0s timeout=1s period=10s #success=1 #failure=3
    Environment:
      VLLM_CPU_KVCACHE_SPACE:  8
      HF_HOME:                 /tmp/hf_home
      INFERENCE_SERVICE_NAME:  tinyllama
    Mounts:
      /mnt/models from kserve-provision-location (ro)
Conditions:
  Type                        Status
  PodReadyToStartContainers   True 
  Initialized                 True 
  Ready                       True 
  ContainersReady             True 
  PodScheduled                True 
Volumes:
  kserve-provision-location:
    Type:       EmptyDir (a temporary directory that shares a pod's lifetime)
    Medium:     
    SizeLimit:  <unset>
  cabundle-cert:
    Type:        ConfigMap (a volume populated by a ConfigMap)
    Name:        odh-kserve-custom-ca-bundle
    Optional:    false
QoS Class:       Burstable
Node-Selectors:  <none>
Tolerations:     node.kubernetes.io/memory-pressure:NoSchedule op=Exists
                 node.kubernetes.io/not-ready:NoExecute op=Exists for 300s
                 node.kubernetes.io/unreachable:NoExecute op=Exists for 300s
Events:
  Type     Reason          Age                   From               Message
  ----     ------          ----                  ----               -------
  Normal   Scheduled       2m42s                 default-scheduler  Successfully assigned model-eval/tinyllama-predictor-7685b8c98f-lds2t to worker-1
  Normal   AddedInterface  2m41s                 multus             Add eth0 [10.131.1.153/23] from ovn-kubernetes
  Normal   Pulled          2m41s                 kubelet            Container image "registry.redhat.io/rhoai/odh-kserve-storage-initializer-rhel9@sha256:b0cb18b34835e27ee75f8c804c2cf5acdf367c45886c248989588d175052a813" already present on machine
  Normal   Created         2m41s                 kubelet            Created container: storage-initializer
  Normal   Started         2m41s                 kubelet            Started container storage-initializer
  Normal   Pulled          2m14s                 kubelet            Container image "registry.redhat.io/rhoai/odh-vllm-cpu-rhel9@sha256:68d4978a9f9ed7e1b1b1001d386e279d546707e49a997680b9dd0232c860b26a" already present on machine
  Normal   Created         2m14s                 kubelet            Created container: kserve-container
  Normal   Started         2m14s                 kubelet            Started container kserve-container
  Warning  Unhealthy       47s (x11 over 2m13s)  kubelet            Readiness probe failed: dial tcp 10.131.1.153:8080: connect: connection refusedLMevalJob::
[root@apar-b-b6fb-bastion-0 ~]# oc get LMEvalJob tinyllamaeval -o yaml
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: LMEvalJob
metadata:
  annotations:
    openshift.io/display-name: tinyllamaeval
  creationTimestamp: "2026-04-09T11:53:27Z"
  finalizers:
  - trustyai.opendatahub.io/lmes-finalizer
  generation: 1
  name: tinyllamaeval
  namespace: model-eval
  resourceVersion: "188872065"
  uid: 49193c59-b68b-4d55-97d4-644b38ae9b04
spec:
  allowCodeExecution: true
  allowOnline: true
  batchSize: "1"
  logSamples: true
  model: local-completions
  modelArgs:
  - name: model
    value: tinyllama
  - name: base_url
    value: https://tinyllama-model-eval.apps.apar-b-b6fb.ibm.com/v1/completions
  - name: num_concurrent
    value: "1"
  - name: max_retries
    value: "3"
  - name: tokenized_requests
    value: "True"
  - name: tokenizer
    value: TinyLlama/TinyLlama-1.1B-Chat-v1.0
  outputs:
    pvcManaged:
      size: 100Mi
  taskList:
    taskNames:
    - arc_easy
status:
  lastScheduleTime: "2026-04-09T11:53:27Z"
  podName: tinyllamaeval
  state: ScheduledAaruni Aggarwal  [1:03 PM]
Could you please let me know if I am missing something?
ThanksAaruni Aggarwal  [1:04 PM]
Sharing complete error:
[root@apar-b-b6fb-bastion-0 ~]# oc get pods
NAME                                   READY   STATUS    RESTARTS   AGE
tinyllama-predictor-7685b8c98f-lds2t   1/1     Running   0          11m
tinyllamaeval                          1/1     Running   0          59s
[root@apar-b-b6fb-bastion-0 ~]# 
[root@apar-b-b6fb-bastion-0 ~]# oc logs -f pod/tinyllamaeval
Defaulted container "main" out of: main, driver (init)
2026-04-09:11:54:01,387 INFO     [__main__:325] Including path: /opt/app-root/src/my_tasks
2026-04-09:11:54:14,673 INFO     [__main__:397] Selected Tasks: ['arc_easy']
2026-04-09:11:54:14,674 INFO     [lm_eval.evaluator:177] Setting random seed to 0 | Setting numpy seed to 1234 | Setting torch manual seed to 1234 | Setting fewshot manual seed to 1234
2026-04-09:11:54:14,674 INFO     [lm_eval.evaluator:214] Initializing local-completions model, with arguments: {'model': 'tinyllama', 'base_url': 'https://tinyllama-model-eval.apps.apar-b-b6fb.ibm.com/v1/completions', 'num_concurrent': 1, 'max_retries': 3, 'tokenized_requests': True, 'tokenizer': 'TinyLlama/TinyLlama-1.1B-Chat-v1.0'}
2026-04-09:11:54:14,674 INFO     [lm_eval.models.api_models:117] Using max length 2048 - 1
2026-04-09:11:54:14,674 INFO     [lm_eval.models.api_models:120] Concurrent requests are disabled. To enable concurrent requests, set `num_concurrent` > 1.
2026-04-09:11:54:14,674 INFO     [lm_eval.models.api_models:159] Using tokenizer huggingface
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-04-09:11:54:14,838 WARNING  [huggingface_hub.file_download:1729] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-04-09:11:54:16,279 WARNING  [huggingface_hub.file_download:1729] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-04-09:11:54:16,363 WARNING  [huggingface_hub.file_download:1729] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
2026-04-09:11:54:16,451 WARNING  [huggingface_hub.file_download:1729] Xet Storage is enabled for this repo, but the 'hf_xet' package is not installed. Falling back to regular HTTP download. For better performance, install the package with: `pip install huggingface_hub[hf_xet]` or `pip install hf_xet`
Generating train split:   0%|          | 0/2251 [00:00<?, ? examples/s]
Generating train split: 100%|██████████| 2251/2251 [00:00<00:00, 419542.23 examples/s]
Generating test split:   0%|          | 0/2376 [00:00<?, ? examples/s]
Generating test split: 100%|██████████| 2376/2376 [00:00<00:00, 554263.98 examples/s]
Generating validation split:   0%|          | 0/570 [00:00<?, ? examples/s]
Generating validation split: 100%|██████████| 570/570 [00:00<00:00, 314035.63 examples/s]
2026-04-09:11:54:16,661 INFO     [lm_eval.api.task:420] Building contexts for arc_easy on rank 0...
  0%|          | 0/2376 [00:00<?, ?it/s]
  6%|▋         | 149/2376 [00:00<00:01, 1485.30it/s]
 13%|█▎        | 299/2376 [00:00<00:01, 1492.46it/s]
 19%|█▉        | 449/2376 [00:00<00:01, 978.23it/s] 
 25%|██▌       | 598/2376 [00:00<00:01, 1129.61it/s]
 31%|███▏      | 746/2376 [00:00<00:01, 1232.38it/s]
 38%|███▊      | 895/2376 [00:00<00:01, 1307.11it/s]
 44%|████▍     | 1043/2376 [00:00<00:00, 1357.97it/s]
 50%|█████     | 1192/2376 [00:00<00:00, 1394.92it/s]
 56%|█████▋    | 1339/2376 [00:01<00:00, 1414.93it/s]
 62%|██████▏   | 1484/2376 [00:01<00:00, 1411.18it/s]
 69%|██████▊   | 1633/2376 [00:01<00:00, 1433.18it/s]
 75%|███████▍  | 1778/2376 [00:01<00:00, 1421.74it/s]
 81%|████████  | 1927/2376 [00:01<00:00, 1441.65it/s]
 87%|████████▋ | 2076/2376 [00:01<00:00, 1455.13it/s]
 94%|█████████▎| 2225/2376 [00:01<00:00, 1463.11it/s]
100%|█████████▉| 2372/2376 [00:01<00:00, 1465.05it/s]
100%|██████████| 2376/2376 [00:01<00:00, 1372.77it/s]
2026-04-09:11:54:18,549 INFO     [lm_eval.evaluator:525] Running loglikelihood requests
Requesting API:   0%|          | 0/9501 [00:00<?, ?it/s]
Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 464, in _make_request
    self._validate_conn(conn)
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 1093, in _validate_conn
    conn.connect()
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 796, in connect
    sock_and_verified = _ssl_wrap_socket_and_match_hostname(
                        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connection.py", line 975, in _ssl_wrap_socket_and_match_hostname
    ssl_sock = ssl_wrap_socket(
               ^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 483, in ssl_wrap_socket
    ssl_sock = _ssl_wrap_socket_impl(sock, context, tls_in_tls, server_hostname)
               ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/ssl_.py", line 527, in _ssl_wrap_socket_impl
    return ssl_context.wrap_socket(sock, server_hostname=server_hostname)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 517, in wrap_socket
    return self.sslsocket_class._create(
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.11/ssl.py", line 1104, in _create
    self.do_handshake()
  File "/usr/lib64/python3.11/ssl.py", line 1382, in do_handshake
    self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 787, in urlopen
    response = self._make_request(
               ^^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 488, in _make_request
    raise new_e
urllib3.exceptions.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)

The above exception was the direct cause of the following exception:

Traceback (most recent call last):
  File "/opt/app-root/lib64/python3.11/site-packages/requests/adapters.py", line 644, in send
    resp = conn.urlopen(
           ^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/connectionpool.py", line 841, in urlopen
    retries = retries.increment(
              ^^^^^^^^^^^^^^^^^^
  File "/opt/app-root/lib64/python3.11/site-packages/urllib3/util/retry.py", line 535, in increment
    raise MaxRetryError(_pool, url, reason) from reason  # type: ignore[arg-type]
    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
urllib3.exceptions.MaxRetryError: HTTPSConnectionPool(host='tinyllama-model-eval.apps.apar-b-b6fb.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "<frozen runpy>", line 198, in _run_module_as_main
  File "<frozen runpy>", line 88, in _run_code
  File "/opt/app-root/src/lm_eval/__main__.py", line 486, in <module>
    cli_evaluate()
  ..
requests.exceptions.SSLError: HTTPSConnectionPool(host='tinyllama-model-eval.apps.apar-b-b6fb.ibm.com', port=443): Max retries exceeded with url: /v1/completions (Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain (_ssl.c:1004)')))
Requesting API:   0%|          | 0/9501 [00:02<?, ?it/s]
2026-04-09T11:54:23Z	INFO	driver	update status: job completed	{"state": {"state":"Complete","reason":"Failed","message":"exit status 1","progressBars":[{"message":"Generating train split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2251/2251"},{"message":"Generating test split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"2376/2376"},{"message":"Generating validation split","percent":"100%","elapsedTime":"0:00:00","remainingTimeEstimate":"0:00:00","count":"570/570"}]}}
2026-04-09T11:54:28Z	ERROR	driver	Driver.Run failed	{"error": "exit status 1"}
main.main
	/go/src/github.com/trustyai-explainability/trustyai-service-operator/cmd/lmes_driver/main.go:150
runtime.main
	/usr/lib/golang/src/runtime/proc.go:272Aaruni Aggarwal  [1:09 PM]
I am aware that we can either set verify_certificate : False  or provide the certificate path to this variable inside LMevalJob, but currently there is no option to configure it via UI. Is this a bug? Do we need to do some manual intervention? I tried editing the job later(till that time it was in completed state with reason being failed). I did edit the LMevalJob and added verify_certificate, but I am not sure, how to reconcile the pod. I deleted the pod, thinking job will re-create it but no new pod was created. (edited) 
lmcfadde  [11:11 PM]
@rui do you have any update on the defect?
morana  [7:23 AM]
@Aaruni Aggarwal yes you are right , its cert issue . But wanna understand , why do you want to handle it as part of UI . it may be a bug
we can apply false through cli
# Recreate it with verify_certificate set
cat <<EOF | oc apply -f -
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: LMEvalJob
metadata:
  name: tinyllama-run
spec:
  model: local-completions
  modelParameters:
    - name: base_url
      value: https://tinyllama-model-deploy.apps.rdr-rhoai-comp.ibm.com/v1/completions
    - name: verify_certificate
      value: "false"
  ...
EOFmorana  [7:24 AM]
To force reconciliation you need to reset the CR state: 
# Option 1 — Patch the status to reset it
oc patch lmevaljob tinyllama-run --type=merge \
  -p '{"status": {"state": "New"}}'

# Option 2 — Remove the status entirely
oc patch lmevaljob tinyllama-run --type=json \
  -p='[{"op": "remove", "path": "/status"}]'morana  [7:25 AM]
Can you try this option..
Aaruni Aggarwal  [7:29 AM]
Hi @morana, I understand that we can set verify_certificate=false via the CLI in the LMEval Job. However, since we are testing the LMEval UI feature, it should ideally work correctly through the UI without requiring any CLI changes, right?
Also, when we create an evaluation run via the UI, it creates the job and the corresponding pod, which fails due to the same certificate error. Updating the LMEvalJob afterward doesn’t reconcile or fix the pod.
If this is a known issue, it should be documented for now, or should be fixed.Aaruni Aggarwal  [7:29 AM]
Can you try this option..will give it a try.
Aaruni Aggarwal  [3:24 PM]
Hi @morana, I tried with the above mentioned approaches to reconcile the CR state, but it's not helping.
[root@rdr-rhoai-bastion-0 kubernetes]# oc get pods
NAME                                  READY   STATUS    RESTARTS   AGE
tinyllama-predictor-c88f79c97-7zxmf   1/1     Running   0          13m
tinyllamaeval                         0/1     Error     0          2m22s
[root@rdr-rhoai-bastion-0 kubernetes]# oc patch lmevaljob tinyllamaeval --type=merge \
  -p '{"status": {"state": "New"}}'
lmevaljob.trustyai.opendatahub.io/tinyllamaeval patched (no change)

[root@rdr-rhoai-bastion-0 kubernetes]# oc get pods
NAME                                  READY   STATUS    RESTARTS   AGE
tinyllama-predictor-c88f79c97-7zxmf   1/1     Running   0          14m
tinyllamaeval                         0/1     Error     0          3m35s

[root@rdr-rhoai-bastion-0 kubernetes]# oc get LMevalJob 
NAME            STATE
tinyllamaeval   Complete

[root@rdr-rhoai-bastion-0 kubernetes]# oc get LMevalJob -o yaml
apiVersion: v1
items:
- apiVersion: trustyai.opendatahub.io/v1alpha1
  kind: LMEvalJob
  metadata:
    annotations:
      openshift.io/display-name: tinyllamaeval
    creationTimestamp: "2026-04-29T14:16:07Z"
    finalizers:
    - trustyai.opendatahub.io/lmes-finalizer
    generation: 2
    name: tinyllamaeval
    namespace: model-eval
    resourceVersion: "13691316"
    uid: 6b537518-b928-49c1-bb24-c105dc8d5985
  spec:
    allowCodeExecution: true
    allowOnline: true
    batchSize: "1"
    logSamples: true
    model: local-completions
    modelArgs:
    - name: model
      value: tinyllama
    - name: base_url
      value: https://tinyllama-model-eval.apps.rdr-rhoai.ibm.com/v1/completions
    - name: num_concurrent
      value: "1"
    - name: max_retries
      value: "3"
    - name: tokenized_requests
      value: "True"
    - name: tokenizer
      value: TinyLlama/TinyLlama-1.1B-Chat-v1.0
    - name: verify_certificate
      value: "False"
    outputs:
      pvcManaged:
        size: 100Mi
    taskList:
      taskNames:
      - arc_easy
  status:
    completeTime: "2026-04-29T14:17:08Z"
    lastScheduleTime: "2026-04-29T14:16:07Z"
    message: exit status 1
    podName: tinyllamaeval
    progressBars:
    - count: 2251/2251
      elapsedTime: "0:00:00"
      message: Generating train split
      percent: 100%
      remainingTimeEstimate: "0:00:00"
    - count: 2376/2376
      elapsedTime: "0:00:00"
      message: Generating test split
      percent: 100%
      remainingTimeEstimate: "0:00:00"
    - count: 570/570
      elapsedTime: "0:00:00"
      message: Generating validation split
      percent: 100%
      remainingTimeEstimate: "0:00:00"
    reason: Failed
    state: Complete
kind: List
metadata:
  resourceVersion: ""

[root@rdr-rhoai-bastion-0 kubernetes]# oc get LMevalJob 
NAME            STATE
tinyllamaeval   Complete

[root@rdr-rhoai-bastion-0 kubernetes]# oc get pods
NAME                                  READY   STATUS    RESTARTS   AGE
tinyllama-predictor-c88f79c97-7zxmf   1/1     Running   0          15m
tinyllamaeval                         0/1     Error     0          5m2s

[root@rdr-rhoai-bastion-0 kubernetes]# oc delete pod tinyllamaeval 
pod "tinyllamaeval" deleted from model-eval namespace

[root@rdr-rhoai-bastion-0 kubernetes]# oc patch lmevaljob tinyllamaeval --type=merge   -p '{"status": {"state": "New"}}'
lmevaljob.trustyai.opendatahub.io/tinyllamaeval patched (no change)

[root@rdr-rhoai-bastion-0 kubernetes]# oc get pods
NAME                                  READY   STATUS    RESTARTS   AGE
tinyllama-predictor-c88f79c97-7zxmf   1/1     Running   0          16m
