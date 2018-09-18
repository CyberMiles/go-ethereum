#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/types.h>
#include <signal.h>
#include <stdlib.h>
#include <fcntl.h>
#include <time.h>

#define SIZE 1024

typedef int64_t* (* func_gas)(char* pArgStr);
typedef char* (* func_run)(char* pArgStr);

void* fork_call(int fid, void* f, char* argsText, int *status);


// f should be op_gas()
uint64_t fork_gas(void* f, char *argsText, int *status){
	void *ret = fork_call(0, f, argsText, status);
    if(ret==NULL) return 0;
	return *((uint64_t*)ret);
}

// f should be op_run()
char* fork_run(void* f, char *argsText, int *status){
	return (char*) fork_call(1, f, argsText, status);
}

// fid==0 if called by fork_gas
// fid==1 if called by fork_run
void* fork_call(int fid, void* f, char* argsText, int *status)
{
    int pfd[2];
    int pid;
    void* ret = malloc(SIZE);
    if (pipe(pfd) == -1){
        *status = 1;
        return NULL;
    }
    if (fcntl(pfd[0], F_SETFL, O_NONBLOCK) < 0){
        *status = 2;
        return NULL;
    }
    if ((pid = fork()) < 0){
        *status = 3;
        return NULL;
    }

    if (pid == 0){ // child
        close(pfd[0]);
        if(fid==0){
            func_gas f_gas= (func_gas) f;
            int64_t* gas = f_gas(argsText);
            char str[22];
            sprintf(str, "%lld", *gas);
            write(pfd[1], str, strlen(str)+1); // (with \0 would end read())
        } else if (fid==1){// op_run
            func_run f_run = (func_run) f;
            char *retText = f_run(argsText);
            if (retText == NULL)
                write(pfd[1], "\0", 1);
            else
                write(pfd[1], retText, strlen(retText)+1); // (with \0 would end read())
        } else {
            exit(7122);
        }
        close(pfd[1]);
        exit(0);
    } else { // parent
        close(pfd[1]);
        int nread, ret_len=0, ret_cap=SIZE;
        char *ptr = (char*)ret;
        int n_iter=30;
        
        struct timespec tim, tim2;
        tim.tv_sec  = 0;
        tim.tv_nsec = 100000000L;

        do {
            nread = read(pfd[0], ptr, ret_cap-ret_len);
            if(nread==-1){
                nanosleep(&tim , &tim2);
                if(n_iter==0){
                    kill(pid, SIGKILL);
                }else if(n_iter>0){
                    n_iter--;
                }
                continue;
            } else {
                ptr+= nread;
                ret_len+= nread;
                if(ret_cap==ret_len){
                    ret_cap*= 2;
                    ret = realloc(ret, ret_cap);
                    ptr = (char*)ret + ret_len;
                }
            }
        } while (0 == waitpid(pid, status, WNOHANG));
        // remainning in pipe
        while (1){
            nread = read(pfd[0], ptr, ret_cap-ret_len);
            if(nread==-1 || nread==0){
                break;
            } else {
                ptr+= nread;
                ret_len+= nread;
                if(ret_cap==ret_len){
                    ret_cap*= 2;
                    ret = realloc(ret, ret_cap);
                    ptr = (char*)ret + ret_len;
                }
            }
        }

        close(pfd[0]);

        
        if ( WIFEXITED(*status) ) {
            const int es = WEXITSTATUS(*status);
            *status = es;
        } else if (WIFSIGNALED(*status)){
            if (WTERMSIG(*status) == SIGSEGV){ // terminated by a segfault
                printf("segfault\n");
            }
        }
        if(fid==0){
            int64_t *gas = (int64_t*) malloc(sizeof (int64_t));
            sscanf((char*)ret, "%lld\n", gas);
            return gas;
        } else {
            return ret;
        }
    }
}
